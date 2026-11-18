package backup

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/teixeira49/oc-profiles/internal/core"
)

const config = `{
  "agent": {
    "gentle-ai-explore": {"description": "Explore.", "hidden": true, "mode": "subagent", "prompt": "explore"},
    "gentle-ai-verify": {"description": "Verify.", "hidden": true, "mode": "subagent", "prompt": "verify"},
    "gentle-ai-worker": {"description": "Worker.", "hidden": true, "mode": "subagent", "prompt": "work"},
    "gentle-orchestrator": {
      "model": "openai/gpt-x",
      "permission": {"task": {"*": "deny", "gentle-ai-explore": "allow", "gentle-ai-verify": "allow", "gentle-ai-worker": "allow", "jd-fix-agent": "allow", "jd-judge-a": "allow", "jd-judge-b": "allow"}},
      "prompt": "Use gentle-ai-explore, gentle-ai-worker and gentle-ai-verify."
    },
    "jd-fix-agent": {"description": "Fix.", "hidden": true, "mode": "subagent", "prompt": "fix"},
    "jd-judge-a": {"description": "Judge A.", "hidden": true, "mode": "subagent", "prompt": "a"},
    "jd-judge-b": {"description": "Judge B.", "hidden": true, "mode": "subagent", "prompt": "b"}
  }
}
`

func setup(t *testing.T) core.Paths {
	t.Helper()
	root := t.TempDir()
	p := core.Paths{
		OpencodeDir: filepath.Join(root, "oc"),
		StoreDir:    filepath.Join(root, "store"),
		BackupDir:   filepath.Join(root, "backup"),
	}
	if err := os.MkdirAll(p.OpencodeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.OpencodeJSON(), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := core.Import(p, core.ImportOptions{}); err != nil {
		t.Fatal(err)
	}
	tick := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	Now = func() time.Time { tick = tick.Add(time.Second); return tick }
	t.Cleanup(func() { Now = time.Now })
	return p
}

func addProfile(t *testing.T, p core.Paths, name string) {
	t.Helper()
	s, err := core.LoadStore(p)
	if err != nil {
		t.Fatal(err)
	}
	s.Profiles.Upsert(core.Profile{Name: name, Models: map[string]core.Assignment{
		core.RoleOrchestrator: {Model: "openai/gpt-x", Variant: "high"},
		core.RoleFixAgent:     {Model: "zai/glm"},
	}})
	if err := s.SaveProfiles(); err != nil {
		t.Fatal(err)
	}
}

func mustSnapshot(t *testing.T, p core.Paths) SnapshotResult {
	t.Helper()
	res, err := Snapshot(p)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestSnapshotWriteReadAndNoop(t *testing.T) {
	p := setup(t)
	addProfile(t, p, "alpha")
	r1 := mustSnapshot(t, p)
	if !r1.Written || r1.Info.Seq != 1 || r1.Info.Profiles != 1 || !r1.Info.Valid {
		t.Fatalf("first snapshot: %+v", r1)
	}
	doc, payload, _, err := Read(filepath.Join(p.BackupDir, "current.json"))
	if err != nil {
		t.Fatal(err)
	}
	if doc.Version != Version || doc.CreatedAt != "2026-11-18T12:00:01Z" || len(doc.SHA256) != 64 {
		t.Fatalf("metadata: %+v", doc)
	}
	if !strings.Contains(string(payload.Profiles), `"alpha"`) || !strings.Contains(string(payload.Templates), "gentle-orchestrator") {
		t.Fatal("payload is not self-contained")
	}
	if LastSeq(p) != 1 {
		t.Fatal("store did not record the sequence")
	}
	// Unchanged store: no new snapshot.
	if r2 := mustSnapshot(t, p); r2.Written || r2.Info.Seq != 1 {
		t.Fatalf("unchanged store wrote a snapshot: %+v", r2)
	}
	// No temp files left behind (atomic write).
	for _, dir := range []string{p.BackupDir, filepath.Join(p.BackupDir, "history")} {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if strings.Contains(e.Name(), ".tmp-") {
				t.Fatalf("temp file left: %s", e.Name())
			}
		}
	}
}

func TestFailedBackupWriteKeepsPreviousSnapshotAndStore(t *testing.T) {
	p := setup(t)
	mustSnapshot(t, p)
	before, _ := os.ReadFile(filepath.Join(p.BackupDir, "current.json"))
	storeBefore, _ := os.ReadFile(p.ProfilesFile())

	addProfile(t, p, "alpha")
	storeAfterSave, _ := os.ReadFile(p.ProfilesFile())
	hist := filepath.Join(p.BackupDir, "history")
	if err := os.Chmod(hist, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(hist, 0o700) })

	if _, err := Snapshot(p); err == nil {
		t.Fatal("expected a write error on a read-only history dir")
	}
	after, _ := os.ReadFile(filepath.Join(p.BackupDir, "current.json"))
	if !bytes.Equal(before, after) {
		t.Fatal("current.json changed by a failed write")
	}
	if _, _, _, err := Read(filepath.Join(p.BackupDir, "current.json")); err != nil {
		t.Fatalf("previous snapshot no longer valid: %v", err)
	}
	if got, _ := os.ReadFile(p.ProfilesFile()); !bytes.Equal(got, storeAfterSave) || bytes.Equal(got, storeBefore) {
		t.Fatal("store was modified by a failed backup")
	}
}

func TestChecksumDetectsCorruptionAndFallsBackToHistory(t *testing.T) {
	p := setup(t)
	mustSnapshot(t, p)
	addProfile(t, p, "alpha")
	mustSnapshot(t, p)

	cur := filepath.Join(p.BackupDir, "current.json")
	data, _ := os.ReadFile(cur)
	tampered := bytes.Replace(data, []byte(`"alpha"`), []byte(`"alphb"`), 1)
	if bytes.Equal(tampered, data) {
		t.Fatal("tamper did not apply")
	}
	if err := os.WriteFile(cur, tampered, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := Read(cur); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("corruption not detected: %v", err)
	}
	// Truncated history file is also skipped.
	if err := os.WriteFile(filepath.Join(p.BackupDir, "history", "snap-000002.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, info, warnings, ok := NewestValid(p.BackupDir)
	if !ok || info.Seq != 1 || len(warnings) != 2 {
		t.Fatalf("fallback: ok=%v info=%+v warnings=%v", ok, info, warnings)
	}
	var corrupt int
	for _, i := range List(p.BackupDir) {
		if !i.Valid {
			corrupt++
		}
	}
	if corrupt != 2 {
		t.Fatalf("list should flag 2 corrupt snapshots, got %d", corrupt)
	}
}

func TestHistoryRotationKeepsTen(t *testing.T) {
	p := setup(t)
	for i := 0; i < 13; i++ {
		addProfile(t, p, "p"+string(rune('a'+i)))
		if r := mustSnapshot(t, p); r.Info.Seq != int64(i+1) {
			t.Fatalf("seq = %d, want %d", r.Info.Seq, i+1)
		}
	}
	entries, _ := os.ReadDir(filepath.Join(p.BackupDir, "history"))
	if len(entries) != MaxHistory {
		t.Fatalf("history has %d files", len(entries))
	}
	list := List(p.BackupDir)
	if list[0].Seq != 13 || !list[0].Current || list[len(list)-1].Seq != 4 {
		t.Fatalf("list = %+v", list)
	}
}

func TestRecoverMissingStore(t *testing.T) {
	p := setup(t)
	addProfile(t, p, "alpha")
	mustSnapshot(t, p)
	orig, _ := os.ReadFile(p.ProfilesFile())
	if err := os.RemoveAll(p.StoreDir); err != nil {
		t.Fatal(err)
	}
	rep, err := Recover(p)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Restored || rep.From.Seq != 1 || rep.Reason != "missing" {
		t.Fatalf("report = %+v", rep)
	}
	if got, _ := os.ReadFile(p.ProfilesFile()); !bytes.Equal(got, orig) {
		t.Fatal("restored profiles.json differs from the original")
	}
	if _, err := core.LoadStore(p); err != nil {
		t.Fatal(err)
	}
}

func TestRecoverCorruptStoreMovesItAside(t *testing.T) {
	p := setup(t)
	addProfile(t, p, "alpha")
	mustSnapshot(t, p)
	if err := os.WriteFile(p.ProfilesFile(), []byte(`{"version": 2, "profiles": [`), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err := Recover(p)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Restored || len(rep.MovedAside) != 2 || !strings.Contains(rep.MovedAside[0], ".corrupt-") {
		t.Fatalf("report = %+v", rep)
	}
	s, err := core.LoadStore(p)
	if err != nil || s.Profiles.Find("alpha") < 0 {
		t.Fatalf("store not recovered: %v", err)
	}
	if data, _ := os.ReadFile(rep.MovedAside[0]); !strings.HasPrefix(string(data), `{"version": 2`) {
		t.Fatal("corrupt file not preserved")
	}
}

func TestRecoverCorruptStoreWithoutBackupFails(t *testing.T) {
	p := setup(t)
	if err := os.WriteFile(p.ProfilesFile(), []byte(`garbage`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Recover(p); err == nil || !strings.Contains(err.Error(), "no valid backup snapshot") {
		t.Fatalf("err = %v", err)
	}
	if data, _ := os.ReadFile(p.ProfilesFile()); string(data) != "garbage" {
		t.Fatal("corrupt store touched without a backup")
	}
}

func TestRecoverWarnsWhenMirrorIsAhead(t *testing.T) {
	p := setup(t)
	mustSnapshot(t, p) // #1
	storeBefore, _ := os.ReadFile(p.ProfilesFile())
	// Another machine/session wrote #2 with different content, store kept #1.
	addProfile(t, p, "alpha")
	mustSnapshot(t, p) // #2, lastSeq=2
	if err := os.WriteFile(p.ProfilesFile(), storeBefore, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := setLastSeq(p, 1); err != nil {
		t.Fatal(err)
	}
	rep, err := Recover(p)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Restored || len(rep.Warnings) != 1 || !strings.Contains(rep.Warnings[0], "snapshot #2") {
		t.Fatalf("report = %+v", rep)
	}
	if got, _ := os.ReadFile(p.ProfilesFile()); !bytes.Equal(got, storeBefore) {
		t.Fatal("store overwritten silently")
	}
	// The next sync makes the store the newest snapshot (#3); #2 stays in history.
	res, err := Sync(p)
	if err != nil || res.BackupErr != nil {
		t.Fatal(err, res.BackupErr)
	}
	if res.Snapshot.Info.Seq != 3 {
		t.Fatalf("seq = %d", res.Snapshot.Info.Seq)
	}
	if _, _, err := Find(p.BackupDir, 2); err != nil {
		t.Fatal("ahead snapshot lost")
	}

	// Ahead but identical content: silent catch-up.
	if err := setLastSeq(p, 0); err != nil {
		t.Fatal(err)
	}
	rep, _ = Recover(p)
	if len(rep.Warnings) != 0 || LastSeq(p) != 3 {
		t.Fatalf("identical mirror should not warn: %+v last=%d", rep, LastSeq(p))
	}
}

func TestSyncRegeneratesMissingAgentsAndBacksUp(t *testing.T) {
	p := setup(t)
	addProfile(t, p, "alpha")
	res, err := Sync(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Apply.Added) != 5 || !res.Snapshot.Written || res.Snapshot.Info.Seq != 1 {
		t.Fatalf("first sync: %+v", res)
	}
	synced, _ := os.ReadFile(p.OpencodeJSON())

	// gentle-ai sync wipes the profile agents.
	if err := os.WriteFile(p.OpencodeJSON(), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err = Sync(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Apply.Added) != 5 || res.Snapshot.Written {
		t.Fatalf("recovery sync: added %v written %v", res.Apply.Added, res.Snapshot.Written)
	}
	if got, _ := os.ReadFile(p.OpencodeJSON()); !bytes.Equal(got, synced) {
		t.Fatal("recovered opencode.json differs")
	}
	res, err = Sync(p)
	if err != nil || !res.Apply.InSync() || res.Snapshot.Written {
		t.Fatalf("third sync not a no-op: %+v %v", res, err)
	}
}

func TestRestoreSeqReplacesStoreAndKeepsPrevious(t *testing.T) {
	p := setup(t)
	mustSnapshot(t, p) // #1: no profiles
	addProfile(t, p, "alpha")
	mustSnapshot(t, p) // #2
	info, _, err := RestoreSeq(p, 1)
	if err != nil {
		t.Fatal(err)
	}
	if info.Seq != 1 {
		t.Fatalf("info = %+v", info)
	}
	s, _ := core.LoadStore(p)
	if len(s.Profiles.Profiles) != 0 {
		t.Fatal("store not restored to #1")
	}
	matches, _ := filepath.Glob(p.ProfilesFile() + ".replaced-*")
	if len(matches) != 1 {
		t.Fatal("previous store not kept")
	}
	if _, _, err := RestoreSeq(p, 99); err == nil {
		t.Fatal("unknown seq accepted")
	}
}
