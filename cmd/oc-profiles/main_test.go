package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const config = `{
  "agent": {
    "gentle-ai-explore": {"description": "Explore.", "hidden": true, "mode": "subagent", "prompt": "explore"},
    "gentle-ai-verify": {"description": "Verify.", "hidden": true, "mode": "subagent", "prompt": "verify"},
    "gentle-ai-worker": {"description": "Worker.", "hidden": true, "mode": "subagent", "prompt": "work"},
    "gentle-orchestrator": {
      "model": "openai/gpt-x",
      "permission": {"task": {"*": "deny", "gentle-ai-explore": "allow", "gentle-ai-verify": "allow", "gentle-ai-worker": "allow", "jd-judge-a": "allow", "jd-judge-b": "allow"}},
      "prompt": "Use gentle-ai-explore, gentle-ai-worker and gentle-ai-verify."
    },
    "jd-judge-a": {"description": "Judge A.", "hidden": true, "mode": "subagent", "prompt": "a"},
    "jd-judge-b": {"description": "Judge B.", "hidden": true, "mode": "subagent", "prompt": "b"},
    "odd-orchestrator-alpha": {"description": "x [oc-profiles]", "mode": "primary", "model": "openai/gpt-y", "prompt": "x"}
  }
}
`

type env struct {
	oc, store, backup string
}

func newEnv(t *testing.T) env {
	t.Helper()
	root := t.TempDir()
	e := env{filepath.Join(root, "oc"), filepath.Join(root, "store"), filepath.Join(root, "backup")}
	if err := os.MkdirAll(e.oc, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.oc, "opencode.json"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	// Never fall back to the real home in tests.
	t.Setenv("OC_PROFILES_OPENCODE_DIR", e.oc)
	t.Setenv("OC_PROFILES_HOME", e.store)
	t.Setenv("OC_PROFILES_BACKUP_DIR", e.backup)
	t.Setenv("OC_PROFILES_VARIANTS_FILE", filepath.Join(root, "variants.json"))
	return e
}

func runCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	err := run(args, &out, &errOut)
	return out.String() + errOut.String(), err
}

func TestSyncBackupListAndRestoreCommands(t *testing.T) {
	e := newEnv(t)
	out, err := runCmd(t, "import")
	if err != nil || !strings.Contains(out, "Backup snapshot #1") {
		t.Fatalf("import: %v\n%s", err, out)
	}
	out, err = runCmd(t, "sync")
	if err != nil || !strings.Contains(out, "Restored 4 missing profile agents") && !strings.Contains(out, "Synced") {
		t.Fatalf("sync: %v\n%s", err, out)
	}
	synced, _ := os.ReadFile(filepath.Join(e.oc, "opencode.json"))

	// Wipe the profile agents, as a gentle-ai sync might; sync restores them.
	if err := os.WriteFile(filepath.Join(e.oc, "opencode.json"), []byte(strings.Replace(config, `,
    "odd-orchestrator-alpha": {"description": "x [oc-profiles]", "mode": "primary", "model": "openai/gpt-y", "prompt": "x"}`, "", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err = runCmd(t, "sync")
	if err != nil || !strings.Contains(out, "Restored 4 missing profile agents") || !strings.Contains(out, "unchanged") {
		t.Fatalf("recovery sync: %v\n%s", err, out)
	}
	if got, _ := os.ReadFile(filepath.Join(e.oc, "opencode.json")); !bytes.Equal(got, synced) {
		t.Fatal("sync did not restore the file byte-for-byte")
	}

	// Rename -> snapshot #2; list shows both.
	if out, err = runCmd(t, "rename", "alpha", "beta"); err != nil || !strings.Contains(out, "Backup snapshot #2 (") {
		t.Fatalf("rename: %v\n%s", err, out)
	}
	out, err = runCmd(t, "backup", "list")
	if err != nil || !strings.Contains(out, "#2 ") || !strings.Contains(out, "#1 ") || !strings.Contains(out, "(current)") {
		t.Fatalf("list: %v\n%s", err, out)
	}

	// Restore requires --yes.
	if _, err := runCmd(t, "backup", "restore", "--seq", "1"); err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("restore without --yes: %v", err)
	}
	out, err = runCmd(t, "backup", "restore", "--seq", "1", "--yes")
	if err != nil || !strings.Contains(out, "Restored the store from snapshot #1") || !strings.Contains(out, "Backup snapshot #3") {
		t.Fatalf("restore: %v\n%s", err, out)
	}
	if got, _ := os.ReadFile(filepath.Join(e.oc, "opencode.json")); !bytes.Equal(got, synced) {
		t.Fatal("restore + sync did not bring back alpha")
	}
}

func TestCommandsAutoRecoverDeletedStore(t *testing.T) {
	e := newEnv(t)
	if _, err := runCmd(t, "import"); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(e.store); err != nil {
		t.Fatal(err)
	}
	// Read-only commands only diagnose: no store is written.
	for _, args := range [][]string{{"list"}, {"apply", "--dry-run"}} {
		_, err := runCmd(t, args...)
		if err == nil || !strings.Contains(err.Error(), "run `oc-profiles sync` to restore it from backup snapshot #1") {
			t.Fatalf("%v: %v", args, err)
		}
		if _, statErr := os.Stat(e.store); !os.IsNotExist(statErr) {
			t.Fatalf("%v mutated the store dir", args)
		}
	}
	out, err := runCmd(t, "backup", "list")
	if err != nil || !strings.Contains(out, "store is missing") || !strings.Contains(out, "#1") {
		t.Fatalf("backup list: %v\n%s", err, out)
	}
	if _, statErr := os.Stat(e.store); !os.IsNotExist(statErr) {
		t.Fatal("backup list mutated the store dir")
	}
	// A mutating command recovers.
	out, err = runCmd(t, "sync")
	if err != nil || !strings.Contains(out, "Recovered the store from backup snapshot #1") {
		t.Fatalf("sync: %v\n%s", err, out)
	}
	if out, err = runCmd(t, "list"); err != nil || !strings.Contains(out, "alpha") {
		t.Fatalf("list: %v\n%s", err, out)
	}
}

func TestRenameRollsBackWhenApplyFails(t *testing.T) {
	e := newEnv(t)
	if _, err := runCmd(t, "import"); err != nil {
		t.Fatal(err)
	}
	if _, err := runCmd(t, "sync"); err != nil {
		t.Fatal(err)
	}
	// The user owns an unmarked agent at a key the renamed profile would need.
	cfgPath := filepath.Join(e.oc, "opencode.json")
	cfg, _ := os.ReadFile(cfgPath)
	cfg = bytes.Replace(cfg, []byte(`"jd-judge-a": {`), []byte(`"odd-worker-beta": {"description": "mine", "mode": "subagent"},
    "jd-judge-a": {`), 1)
	if err := os.WriteFile(cfgPath, cfg, 0o644); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(e.store, "profiles.json"))
	_, err := runCmd(t, "rename", "alpha", "beta")
	if err == nil || !strings.Contains(err.Error(), "rename rolled back") || !strings.Contains(err.Error(), `"odd-worker-beta"`) {
		t.Fatalf("err = %v", err)
	}
	if after, _ := os.ReadFile(filepath.Join(e.store, "profiles.json")); !bytes.Equal(before, after) {
		t.Fatal("store not rolled back")
	}
	if got, _ := os.ReadFile(cfgPath); !bytes.Equal(got, cfg) {
		t.Fatal("opencode.json changed by a failed rename")
	}
}
