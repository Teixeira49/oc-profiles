package backup

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Teixeira49/oc-profiles/internal/core"
)

// stateFile records, inside the store dir, the last snapshot sequence the
// store was mirrored as. It is deliberately not part of the payload.
func stateFile(p core.Paths) string { return filepath.Join(p.StoreDir, "backup-state.json") }

type state struct {
	LastSeq int64 `json:"lastSeq"`
}

// LastSeq returns the last snapshot sequence recorded by the store (0 if none).
func LastSeq(p core.Paths) int64 {
	data, err := os.ReadFile(stateFile(p))
	if err != nil {
		return 0
	}
	var s state
	if json.Unmarshal(data, &s) != nil {
		return 0
	}
	return s.LastSeq
}

func setLastSeq(p core.Paths, seq int64) error {
	data, _ := json.Marshal(state{LastSeq: seq})
	if err := os.MkdirAll(p.StoreDir, 0o755); err != nil {
		return err
	}
	return writeAtomic(stateFile(p), append(data, '\n'))
}

// SnapshotResult reports a mirror write.
type SnapshotResult struct {
	Info    Info
	Written bool // false when the mirror already held this exact store
}

// Snapshot mirrors the current on-disk store into p.BackupDir. A failure
// never touches the store; callers must surface the error loudly.
func Snapshot(p core.Paths) (SnapshotResult, error) {
	profiles, templates, err := core.ReadStoreFiles(p)
	if err != nil {
		return SnapshotResult{}, fmt.Errorf("backup: read store: %w", err)
	}
	info, written, err := Write(p.BackupDir, profiles, templates)
	if err != nil {
		return SnapshotResult{Info: info, Written: written}, fmt.Errorf("backup: write %s: %w", p.BackupDir, err)
	}
	if err := setLastSeq(p, info.Seq); err != nil {
		return SnapshotResult{Info: info, Written: written}, fmt.Errorf("backup: record sequence: %w", err)
	}
	return SnapshotResult{Info: info, Written: written}, nil
}

// RecoveryReport describes what Recover did.
type RecoveryReport struct {
	Restored   bool
	From       Info
	Reason     string   // "missing" or the load error
	MovedAside []string // previous store files kept for inspection
	Warnings   []string
}

// StoreProblem is the read-only diagnosis used by commands that must not
// mutate anything.
type StoreProblem struct {
	Missing  bool
	LoadErr  error // corrupt/invalid store (nil when missing or fine)
	Snapshot Info  // newest snapshot that could repair it
	CanFix   bool
}

// Diagnose reports whether the store needs recovery, without touching it.
func Diagnose(p core.Paths) StoreProblem {
	var pr StoreProblem
	if !core.StoreExists(p) {
		pr.Missing = true
	} else if _, err := core.LoadStore(p); err != nil {
		if errors.Is(err, core.ErrLegacyStore) || errors.Is(err, core.ErrNewerStore) {
			return pr
		}
		pr.LoadErr = err
	} else {
		return pr
	}
	if _, info, ok := newestRestorable(p); ok {
		pr.Snapshot, pr.CanFix = info, true
	}
	return pr
}

// NeedsRecovery reports whether the store is missing/corrupt and a snapshot
// can repair it.
func (pr StoreProblem) NeedsRecovery() bool { return pr.CanFix && (pr.Missing || pr.LoadErr != nil) }

// newestRestorable returns the highest-sequence snapshot whose payload also
// parses as a valid store (not only a valid checksum).
func newestRestorable(p core.Paths) (*core.Store, Info, bool) {
	s, info, _, ok := pickRestorable(p)
	return s, info, ok
}

func pickRestorable(p core.Paths) (*core.Store, Info, []string, bool) {
	valid, warnings := Candidates(p.BackupDir)
	for _, cand := range valid {
		_, payload, info, err := Read(cand.Path)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("skipped %s: %v", cand.Path, err))
			continue
		}
		s, err := parsePayload(p, payload)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("skipped snapshot #%d: %v", info.Seq, err))
			continue
		}
		return s, info, warnings, true
	}
	return nil, Info{}, warnings, false
}

func parsePayload(p core.Paths, payload Payload) (*core.Store, error) {
	templates := []byte(payload.Templates)
	if string(templates) == "null" {
		templates = nil
	}
	return core.ParseStorePayload(p, payload.Profiles, templates)
}

// Recover runs before every mutating command and at TUI start.
//
// Rule:
//   - Store missing or unreadable (not a legacy v1 store, not written by a
//     newer oc-profiles): restore it from the highest-sequence snapshot that
//     verifies and parses (current.json and history). Previous files are
//     renamed to *.corrupt-<unique>, never deleted.
//   - Store written by a newer oc-profiles: refuse; nothing is moved.
//   - Store valid: the store wins. If the newest valid snapshot has a higher
//     sequence than the store recorded and different content, warn and leave
//     both untouched; the next sync/save writes the store as a newer
//     snapshot (the other one stays in history and can be restored with
//     `oc-profiles backup restore --seq N --yes`).
func Recover(p core.Paths) (RecoveryReport, error) {
	var rep RecoveryReport
	missing := !core.StoreExists(p)
	var loadErr error
	if !missing {
		_, loadErr = core.LoadStore(p)
		switch {
		case errors.Is(loadErr, core.ErrLegacyStore):
			return rep, nil // migration handles v1 stores
		case errors.Is(loadErr, core.ErrNewerStore):
			return rep, fmt.Errorf("store was written by a newer oc-profiles; refusing to touch it (%v)", loadErr)
		}
	}

	if missing || loadErr != nil {
		s, info, warnings, ok := pickRestorable(p)
		rep.Warnings = append(rep.Warnings, warnings...)
		if !ok {
			if loadErr != nil {
				return rep, fmt.Errorf("store is corrupt (%v) and no valid backup snapshot exists in %s", loadErr, p.BackupDir)
			}
			return rep, nil // first run: nothing to recover
		}
		rep.Reason = "missing"
		if loadErr != nil {
			rep.Reason = loadErr.Error()
		}
		moved, err := swapInStore(p, s, "corrupt")
		rep.MovedAside = moved
		if err != nil {
			return rep, err
		}
		if err := setLastSeq(p, info.Seq); err != nil {
			rep.Warnings = append(rep.Warnings, "could not record the backup sequence: "+err.Error())
		}
		rep.Restored, rep.From = true, info
		return rep, nil
	}

	// Store is fine: check whether the mirror is ahead of it.
	_, curInfo, warnings, ok := NewestValid(p.BackupDir)
	rep.Warnings = append(rep.Warnings, warnings...)
	if !ok || curInfo.Seq <= LastSeq(p) {
		return rep, nil
	}
	profiles, templates, err := core.ReadStoreFiles(p)
	if err != nil {
		return rep, err
	}
	payload, err := buildPayload(profiles, templates)
	if err != nil {
		return rep, err
	}
	if sum, _ := checksum(payload); sum == curInfo.SHA256 {
		_ = setLastSeq(p, curInfo.Seq) // same content, just catch up
		return rep, nil
	}
	rep.Warnings = append(rep.Warnings, fmt.Sprintf(
		"backup snapshot #%d (%s) is newer than the store (last mirrored #%d) and differs; keeping the store. "+
			"To use the snapshot: oc-profiles backup restore --seq %d --yes",
		curInfo.Seq, curInfo.CreatedAt, LastSeq(p), curInfo.Seq))
	return rep, nil
}

// swapInStore stages both store files from s next to the store, then moves
// the existing files aside (*.<label>-<unique>) and the staged files into
// place. If any step fails, completed steps are rolled back so the store is
// never left half-replaced.
func swapInStore(p core.Paths, s *core.Store, label string) ([]string, error) {
	profiles, err := s.MarshalProfiles()
	if err != nil {
		return nil, err
	}
	templates, err := s.MarshalTemplates()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(p.StoreDir, 0o755); err != nil {
		return nil, err
	}
	type pair struct{ final, staged, aside string }
	pairs := []pair{
		{final: p.ProfilesFile(), staged: core.AsidePath(p.ProfilesFile(), "staged")},
		{final: p.TemplatesFile(), staged: core.AsidePath(p.TemplatesFile(), "staged")},
	}
	contents := [][]byte{profiles, templates}
	cleanupStaged := func() {
		for _, pr := range pairs {
			os.Remove(pr.staged)
		}
	}
	for i, pr := range pairs {
		if err := writeAtomic(pr.staged, contents[i]); err != nil {
			cleanupStaged()
			return nil, err
		}
		_ = os.Chmod(pr.staged, 0o644)
	}
	var done []pair
	rollback := func() {
		for i := len(done) - 1; i >= 0; i-- {
			d := done[i]
			if d.aside != "" {
				os.Rename(d.aside, d.final)
			} else {
				os.Remove(d.final)
			}
		}
		cleanupStaged()
	}
	var moved []string
	for _, pr := range pairs {
		if _, err := os.Lstat(pr.final); err == nil {
			pr.aside = core.AsidePath(pr.final, label)
			if err := os.Rename(pr.final, pr.aside); err != nil {
				rollback()
				return nil, err
			}
		}
		if err := os.Rename(pr.staged, pr.final); err != nil {
			if pr.aside != "" {
				os.Rename(pr.aside, pr.final)
			}
			rollback()
			return nil, err
		}
		done = append(done, pr)
		if pr.aside != "" {
			moved = append(moved, pr.aside)
		}
	}
	return moved, nil
}

// RestoreSeq replaces the store with snapshot seq, or with the newest
// restorable snapshot when seq is 0 (falling back to older ones whose
// payload parses). The previous store files are kept as *.replaced-<unique>.
func RestoreSeq(p core.Paths, seq int64) (Info, []string, error) {
	var s *core.Store
	var info Info
	var warnings []string
	if seq == 0 {
		var ok bool
		s, info, warnings, ok = pickRestorable(p)
		if !ok {
			return info, warnings, fmt.Errorf("no restorable snapshot in %s", p.BackupDir)
		}
	} else {
		payload, found, err := Find(p.BackupDir, seq)
		if err != nil {
			return found, nil, err
		}
		info = found
		if s, err = parsePayload(p, payload); err != nil {
			return info, nil, fmt.Errorf("snapshot #%d: %w", info.Seq, err)
		}
	}
	if _, err := swapInStore(p, s, "replaced"); err != nil {
		return info, warnings, err
	}
	return info, warnings, setLastSeq(p, info.Seq)
}

// SyncResult reports a full sync.
type SyncResult struct {
	Recovery RecoveryReport
	Apply    core.ApplyResult
	Snapshot SnapshotResult
	// BackupErr is set when opencode.json was reconciled but the mirror write
	// failed; the store and opencode.json are still consistent.
	BackupErr error
}

// Sync recovers the store if needed, reconciles opencode.json (regenerating
// any missing profile agents) and writes a backup snapshot.
func Sync(p core.Paths) (SyncResult, error) {
	var res SyncResult
	rep, err := Recover(p)
	res.Recovery = rep
	if err != nil {
		return res, err
	}
	s, err := core.LoadStore(p)
	if err != nil {
		return res, err
	}
	res.Apply, err = s.Apply()
	if err != nil {
		return res, err
	}
	res.Snapshot, res.BackupErr = Snapshot(p)
	return res, nil
}
