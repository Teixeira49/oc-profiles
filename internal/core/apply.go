package core

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/teixeira49/oc-profiles/internal/ojson"
)

// ApplyResult describes what apply changed (or would change in a dry run).
type ApplyResult struct {
	Added        []string // profile agents created
	Updated      []string // profile agents rewritten
	Removed      []string // stale profile / retired SDD agents deleted
	RemovedFiles []string // retired SDD asset files deleted (relative to opencode dir)
	Warnings     []string
	Changed      bool   // opencode.json content changed
	Backup       string // backup path when a write happened
	// CleanupPending is true while the one-time SDD cleanup has not run.
	CleanupPending bool
	// StoreChanged is true when apply rewrote store files (cleanup flag
	// cleared); callers should refresh the backup mirror.
	StoreChanged bool
}

// MissingCount is the number of profile agents apply would create.
func (r ApplyResult) MissingCount() int { return len(r.Added) }

// InSync reports whether apply would be a no-op.
func (r ApplyResult) InSync() bool {
	return !r.Changed && len(r.RemovedFiles) == 0 && !r.CleanupPending && !r.StoreChanged
}

// MaxOpencodeBackups is how many opencode.json.bak-oc-profiles-* files are kept.
const MaxOpencodeBackups = 10

// ErrConcurrentChange is returned when opencode.json changes between the read
// and the write of an apply.
var ErrConcurrentChange = errors.New("opencode.json changed during apply; re-run")

// testHookBeforeWrite runs between compute and the concurrency check (tests).
var testHookBeforeWrite func()

// Now is the clock used for backup names; tests may override it.
var Now = time.Now

// Plan computes what Apply would do without writing anything.
func (s *Store) Plan() (ApplyResult, error) {
	res, _, _, _, err := s.compute()
	return res, err
}

// Apply reconciles opencode.json with the store, using the live gentle-ai
// agents as templates. It validates the result, writes a timestamped backup
// and replaces the file atomically; nothing is written when the content is
// unchanged, validation fails, or the file changed while apply was running.
func (s *Store) Apply() (ApplyResult, error) {
	res, out, orig, sources, err := s.compute()
	if err != nil {
		return res, err
	}
	if res.Changed {
		path := s.Paths.OpencodeJSON()
		if testHookBeforeWrite != nil {
			testHookBeforeWrite()
		}
		// Concurrency guard: refuse if another writer touched the file.
		if now, err := os.ReadFile(path); err != nil || !bytes.Equal(now, orig) {
			return res, ErrConcurrentChange
		}
		stamp := Now().Format("20060102150405")
		backup := nextBackupName(path, stamp)
		if err := os.WriteFile(backup, orig, 0o600); err != nil {
			return res, fmt.Errorf("write backup: %w", err)
		}
		res.Backup = backup
		if err := writeFileAtomic(path, out, 0o644); err != nil {
			return res, fmt.Errorf("write %s: %w", filepath.Base(path), err)
		}
		if err := rotateOpencodeBackups(path); err != nil {
			res.Warnings = append(res.Warnings, "could not rotate opencode.json backups: "+err.Error())
		}
	}

	if s.Profiles.SDDCleanup {
		removed, warnings, err := removeLegacyAssets(s.Paths, false)
		res.RemovedFiles = removed
		res.Warnings = append(res.Warnings, warnings...)
		if err != nil {
			return res, err
		}
		s.Profiles.SDDCleanup = false
		if err := s.SaveProfiles(); err != nil {
			return res, err
		}
		res.StoreChanged = true
		if err := removeLegacyDir(s.Paths); err != nil {
			res.Warnings = append(res.Warnings, "could not remove legacy snapshot: "+err.Error())
		}
		res.CleanupPending = false
	}

	// Refresh the fallback snapshot with the live sources that were used.
	if !sameJSON(sources, s.Sources) {
		s.Sources = sources
		if err := s.SaveTemplates(); err != nil {
			return res, fmt.Errorf("update template snapshot: %w", err)
		}
		res.StoreChanged = true
	}
	return res, nil
}

// removeLegacyDir deletes only the known children of legacy/ and then the
// directory itself if it is empty.
func removeLegacyDir(p Paths) error {
	if err := os.RemoveAll(p.legacyAssetsDir()); err != nil {
		return err
	}
	if err := os.Remove(p.legacyTemplatesFile()); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Remove(p.LegacyDir()); err != nil && !os.IsNotExist(err) {
		if entries, rerr := os.ReadDir(p.LegacyDir()); rerr == nil && len(entries) > 0 {
			return nil // user files inside: keep the directory
		}
		return err
	}
	return nil
}

// rotateOpencodeBackups keeps the newest MaxOpencodeBackups
// opencode.json.bak-oc-profiles-* files (names sort by timestamp).
func rotateOpencodeBackups(path string) error {
	matches, err := filepath.Glob(path + ".bak-oc-profiles-*")
	if err != nil {
		return err
	}
	if len(matches) <= MaxOpencodeBackups {
		return nil
	}
	sort.Slice(matches, func(i, j int) bool { return backupKey(matches[i]) > backupKey(matches[j]) })
	for _, m := range matches[MaxOpencodeBackups:] {
		if err := os.Remove(m); err != nil {
			return err
		}
	}
	return nil
}

// nextBackupName returns a fresh backup name for stamp, numbering after the
// highest existing one so rotation never causes a name to be reused.
func nextBackupName(path, stamp string) string {
	base := fmt.Sprintf("%s.bak-oc-profiles-%s", path, stamp)
	matches, _ := filepath.Glob(base + "*")
	if len(matches) == 0 {
		return base
	}
	highest := 0
	for _, m := range matches {
		if n, err := strconv.Atoi(strings.TrimPrefix(m, base+"-")); err == nil && n > highest {
			highest = n
		}
	}
	return fmt.Sprintf("%s-%d", base, highest+1)
}

// backupKey orders "...-20260102030405" before "...-20260102030405-1".
func backupKey(name string) string {
	i := strings.LastIndex(name, ".bak-oc-profiles-")
	stamp := name[i+len(".bak-oc-profiles-"):]
	ts, n, _ := strings.Cut(stamp, "-")
	if len(n) < 6 {
		n = strings.Repeat("0", 6-len(n)) + n
	}
	return ts + "-" + n
}

// compute builds the new opencode.json in memory.
func (s *Store) compute() (ApplyResult, []byte, []byte, *ojson.Object, error) {
	res := ApplyResult{CleanupPending: s.Profiles.SDDCleanup}
	doc, agents, orig, err := ReadOpencode(s.Paths)
	if err != nil {
		return res, nil, nil, nil, err
	}
	if !doc.Has("agent") {
		if len(s.Profiles.Profiles) > 0 {
			return res, nil, nil, nil, fmt.Errorf("%s has no \"agent\" section; is gentle-ai installed for OpenCode?", s.Paths.OpencodeJSON())
		}
		doc.Set("agent", agents)
	}

	sources, warnings := s.resolveSources(agents)
	res.Warnings = append(res.Warnings, warnings...)

	var legacy legacyTemplates
	if s.Profiles.SDDCleanup {
		legacy = loadLegacyTemplates(s.Paths)
	}
	// owned: the agent at key may be overwritten or deleted by apply.
	owned := func(key string, agent any) bool {
		obj, ok := agent.(*ojson.Object)
		if !ok {
			return false
		}
		return hasMarker(obj) || (s.Profiles.SDDCleanup && legacy.profile != nil && legacy.owns(key, obj))
	}

	wanted := map[string]bool{}
	var conflicts []string
	for _, p := range s.Profiles.Profiles {
		if err := ValidateProfileName(p.Name); err != nil {
			return res, nil, nil, nil, err
		}
		for _, r := range p.ActiveRoles() {
			src, ok := sources.GetObject(r.Source)
			if !ok {
				return res, nil, nil, nil, fmt.Errorf("profile %s: gentle-ai agent %q is missing from opencode.json and from the store snapshot", p.Name, r.Source)
			}
			key := r.AgentKey(p.Name)
			wanted[key] = true
			val := RenderAgent(r, src, p)
			if cur, ok := agents.Get(key); ok {
				if !owned(key, cur) {
					conflicts = append(conflicts, fmt.Sprintf("profile %s: agent %q already exists and is not managed by oc-profiles (no %s marker in its description); rename or remove it, or rename the profile", p.Name, key, Marker))
					continue
				}
				if !sameJSON(cur, val) {
					res.Updated = append(res.Updated, key)
				}
				agents.Set(key, val)
				continue
			}
			res.Added = append(res.Added, key)
			insertNear(agents, key, val)
		}
	}
	if len(conflicts) > 0 {
		return res, nil, nil, nil, fmt.Errorf("refusing to overwrite agents not managed by oc-profiles:\n  - %s", strings.Join(conflicts, "\n  - "))
	}

	for _, key := range agents.Keys() {
		if _, _, ok := SplitProfileKey(key); !ok || wanted[key] {
			continue
		}
		cur, _ := agents.Get(key)
		if !owned(key, cur) {
			res.Warnings = append(res.Warnings, fmt.Sprintf("kept %q: not managed by oc-profiles (no %s marker)", key, Marker))
			continue
		}
		agents.Delete(key)
		res.Removed = append(res.Removed, key)
	}

	if s.Profiles.SDDCleanup {
		res.Removed = append(res.Removed, removeLegacyAgents(legacy, agents, &res.Warnings)...)
		files, w, err := removeLegacyAssets(s.Paths, true)
		if err != nil {
			return res, nil, nil, nil, err
		}
		res.RemovedFiles = files
		res.Warnings = append(res.Warnings, w...)
	}

	if err := Validate(agents, s.Profiles); err != nil {
		return res, nil, nil, nil, err
	}

	out, err := ojson.MarshalOpts(doc, ojson.UsesHTMLEscaping(orig))
	if err != nil {
		return res, nil, nil, nil, err
	}
	if bytes.HasSuffix(orig, []byte("\n")) {
		out = append(out, '\n')
	}
	if _, err := ojson.ParseObject(out); err != nil {
		return res, nil, nil, nil, fmt.Errorf("generated invalid JSON: %w", err)
	}
	res.Changed = !bytes.Equal(out, orig)
	if res.Changed {
		// A formatting-only difference (another tool's indentation or
		// escaping) is not a change: never rewrite the file for it.
		if prev, err := ojson.ParseObject(orig); err == nil && sameJSON(prev, doc) {
			res.Changed = false
			out = orig
		}
	}
	return res, out, orig, sources, nil
}

// resolveSources prefers the live gentle-ai agents and falls back to the
// store snapshot (with a warning) for any that are missing.
func (s *Store) resolveSources(agents *ojson.Object) (*ojson.Object, []string) {
	out := ojson.NewObject()
	var warnings []string
	for _, name := range SourceNames() {
		if live, ok := agents.GetObject(name); ok {
			out.Set(name, ojson.CloneObject(live))
			continue
		}
		if snap, ok := s.Sources.GetObject(name); ok {
			out.Set(name, ojson.CloneObject(snap))
			warnings = append(warnings, fmt.Sprintf("gentle-ai agent %q is missing from opencode.json; using the stored snapshot", name))
		}
	}
	return out, warnings
}

// removeLegacyAgents deletes retired SDD agents (sdd-orchestrator-*,
// sdd-<phase>[-*]) plus the explore/general entries restored from gentle-ai
// 3.7, but only those provably generated by gentle-ai or by oc-profiles v1:
// carrying gentle-ai's SDD marker or equal to the v1 snapshot. Anything else
// is kept with a warning.
func removeLegacyAgents(lt legacyTemplates, agents *ojson.Object, warnings *[]string) []string {
	var removed []string
	for _, key := range agents.Keys() {
		if !IsLegacySDDKey(key) && key != "explore" && key != "general" {
			continue
		}
		agent, ok := agents.GetObject(key)
		if ok && lt.owns(key, agent) {
			agents.Delete(key)
			removed = append(removed, key)
			continue
		}
		*warnings = append(*warnings, fmt.Sprintf("kept agent %q: it does not match the gentle-ai 3.7 snapshot (customized or not generated)", key))
	}
	return removed
}

// removeLegacyAssets deletes the SDD prompt/skill files whose content still
// matches the v1 snapshot. Changed files are kept with a warning.
func removeLegacyAssets(p Paths, dryRun bool) ([]string, []string, error) {
	snapRoot := p.legacyAssetsDir()
	if !dirExists(snapRoot) {
		return nil, nil, nil
	}
	rels, err := listFiles(snapRoot, ".")
	if err != nil {
		return nil, nil, err
	}
	var removed, warnings []string
	dirs := map[string]bool{}
	for _, rel := range rels {
		target := filepath.Join(p.OpencodeDir, rel)
		cur, err := os.ReadFile(target)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return removed, warnings, err
		}
		snap, err := os.ReadFile(filepath.Join(snapRoot, rel))
		if err != nil {
			return removed, warnings, err
		}
		if !bytes.Equal(cur, snap) {
			warnings = append(warnings, "kept modified SDD file "+rel)
			continue
		}
		if !dryRun {
			if err := os.Remove(target); err != nil {
				return removed, warnings, err
			}
		}
		removed = append(removed, rel)
		for d := filepath.Dir(rel); d != "." && d != "skills" && d != "prompts" && d != filepath.Join("skills", "_shared"); d = filepath.Dir(d) {
			dirs[d] = true
		}
	}
	if !dryRun {
		// Remove now-empty directories, deepest first.
		list := make([]string, 0, len(dirs))
		for d := range dirs {
			list = append(list, d)
		}
		sort.Slice(list, func(i, j int) bool { return strings.Count(list[i], "/") > strings.Count(list[j], "/") })
		for _, d := range list {
			full := filepath.Join(p.OpencodeDir, d)
			if entries, err := os.ReadDir(full); err == nil {
				if len(entries) == 0 {
					_ = os.Remove(full)
				} else if strings.Count(d, string(filepath.Separator)) == 1 {
					warnings = append(warnings, "kept non-empty directory "+d)
				}
			}
		}
	}
	return removed, warnings, nil
}

func sameJSON(a, b any) bool {
	x, err1 := ojson.Marshal(a)
	y, err2 := ojson.Marshal(b)
	return err1 == nil && err2 == nil && bytes.Equal(x, y)
}

// insertNear inserts key before the first existing key that sorts after it,
// which keeps alphabetically grouped agent maps grouped.
func insertNear(agents *ojson.Object, key string, val any) {
	for i, k := range agents.Keys() {
		if k > key {
			agents.InsertAt(i, key, val)
			return
		}
	}
	agents.Set(key, val)
}
