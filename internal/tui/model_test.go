package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Teixeira49/oc-profiles/internal/catalog"
	"github.com/Teixeira49/oc-profiles/internal/core"
)

const fixtureConfig = `{
  "agent": {
    "gentle-ai-explore": {"description": "Explore.", "hidden": true, "mode": "subagent", "prompt": "explore"},
    "gentle-ai-verify": {"description": "Verify.", "hidden": true, "mode": "subagent", "prompt": "verify"},
    "gentle-ai-worker": {"description": "Worker.", "hidden": true, "mode": "subagent", "prompt": "work"},
    "gentle-orchestrator": {
      "model": "openai/gpt-x",
      "permission": {"task": {"*": "deny", "gentle-ai-explore": "allow", "gentle-ai-verify": "allow", "gentle-ai-worker": "allow", "jd-fix-agent": "allow", "jd-judge-a": "allow", "jd-judge-b": "allow"}},
      "prompt": "Bind to ` + "`gentle-orchestrator`" + `; use gentle-ai-explore, gentle-ai-worker, gentle-ai-verify."
    },
    "jd-judge-a": {"description": "Judge A.", "hidden": true, "mode": "subagent", "prompt": "a"},
    "jd-judge-b": {"description": "Judge B.", "hidden": true, "mode": "subagent", "prompt": "b"},
    "jd-fix-agent": {"description": "Fix.", "hidden": true, "mode": "subagent", "prompt": "fix"},
    "odd-orchestrator-base": {"description": "ODD Orchestrator (base profile) [oc-profiles]", "mode": "primary", "model": "openai/gpt-x", "prompt": "x"}
  }
}
`

func fakeLoader(context.Context) (catalog.Catalog, error, error) {
	return catalog.Catalog{
		Providers: map[string][]string{"openai": {"gpt-x", "gpt-y"}, "zai": {"glm"}},
		Variants:  map[string]map[string][]string{"openai": {"gpt-y": {"low", "high"}}},
	}, nil, nil
}

func setup(t *testing.T) core.Paths {
	t.Helper()
	root := t.TempDir()
	p := core.Paths{OpencodeDir: filepath.Join(root, "oc"), StoreDir: filepath.Join(root, "store"), BackupDir: filepath.Join(root, "backup")}
	if err := os.MkdirAll(p.OpencodeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.OpencodeJSON(), []byte(fixtureConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func press(t *testing.T, m Model, keys ...string) Model {
	t.Helper()
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "backspace":
			msg = tea.KeyMsg{Type: tea.KeyBackspace}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		next, _ := m.Update(msg)
		m = next.(Model)
	}
	return m
}

func toContinue(t *testing.T, m Model) Model {
	for i := 0; i < 15; i++ {
		m = press(t, m, "j")
	}
	return press(t, m, "k", "enter")
}

func readConfig(t *testing.T, p core.Paths) string {
	data, err := os.ReadFile(p.OpencodeJSON())
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestImportCreateEditDeleteFlow(t *testing.T) {
	p := setup(t)
	m := New(p, fakeLoader)
	if m.screen != screenImport {
		t.Fatalf("screen = %v, want import", m.screen)
	}
	cat, _, _ := fakeLoader(context.Background())
	next, _ := m.Update(catalogMsg{cat: cat})
	m = next.(Model)

	m = press(t, m, "enter")
	if m.screen != screenList || len(m.store.Profiles.Profiles) != 1 {
		t.Fatalf("import failed: %s", m.errMsg)
	}
	if v := m.View(); !strings.Contains(v, "ODD Profiles") || !strings.Contains(v, "base") {
		t.Fatal("list view wrong")
	}

	// Create "new-one": orchestrator -> openai/gpt-y [high]; subagents -> zai/glm.
	m = press(t, m, "n", "n", "e", "w", "-", "o", "n", "e", "enter")
	if m.screen != screenPicker || m.draft.Name != "new-one" {
		t.Fatalf("name step failed: %v %q %s", m.screen, m.draft.Name, m.nameErr)
	}
	m = press(t, m, "enter", "enter", "j", "enter")
	if m.picker.mode != modeEffortSelect {
		t.Fatalf("expected effort select, got %v", m.picker.mode)
	}
	m = press(t, m, "j", "j", "enter")
	if got := m.assign[core.RoleOrchestrator]; got != (core.Assignment{Model: "openai/gpt-y", Variant: "high"}) {
		t.Fatalf("assignment = %+v", got)
	}
	m = press(t, m, "j", "enter", "j", "enter", "enter")
	for _, r := range core.SubagentRoleIDs() {
		if m.assign[r].Model != "zai/glm" {
			t.Fatalf("set all failed: %+v", m.assign)
		}
	}
	// Judge A -> zai/glm (rows: 0 orch, 1 set all, 2-4 subagents, 5 separator, 6 judge A).
	m = press(t, m, "j", "j", "j", "j")
	if m.cursor != 6 {
		t.Fatalf("separator not skipped, cursor = %d", m.cursor)
	}
	m = press(t, m, "enter", "j", "enter", "enter")
	// Fix Agent row (index 8) -> openai/gpt-x.
	m = press(t, m, "j", "j")
	if m.cursor != 8 {
		t.Fatalf("fix agent row cursor = %d", m.cursor)
	}
	m = press(t, m, "enter", "enter", "enter")
	if m.assign[core.RoleFixAgent].Model != "openai/gpt-x" {
		t.Fatalf("fix agent = %+v", m.assign[core.RoleFixAgent])
	}
	m = toContinue(t, m)
	if m.screen != screenConfirm {
		t.Fatalf("screen = %v", m.screen)
	}
	m = press(t, m, "enter")
	if m.errMsg != "" {
		t.Fatalf("save failed: %s", m.errMsg)
	}
	cfg := readConfig(t, p)
	for _, want := range []string{`"odd-orchestrator-new-one"`, `"odd-worker-new-one"`, `"jd-judge-a-new-one"`, `"jd-fix-agent-new-one"`, "use odd-explore-new-one, odd-worker-new-one, odd-verify-new-one."} {
		if !strings.Contains(cfg, want) {
			t.Fatalf("config missing %s", want)
		}
	}
	if strings.Contains(cfg, `"jd-judge-b-new-one"`) {
		t.Fatal("unassigned judge generated")
	}

	// Edit: clear judge A and the fix agent back to global.
	m.cursor = m.store.Profiles.Find("new-one")
	m = press(t, m, "enter", "enter")
	m.cursor = 6
	m = press(t, m, "backspace")
	m.cursor = 8
	m = press(t, m, "backspace")
	if !strings.Contains(m.View(), "(global)") {
		t.Fatal("cleared row should read (global)")
	}
	m = toContinue(t, m)
	m = press(t, m, "enter")
	cfg = readConfig(t, p)
	if strings.Contains(cfg, `"jd-judge-a-new-one"`) || strings.Contains(cfg, `"jd-fix-agent-new-one"`) {
		t.Fatal("cleared judge/fix agent still present")
	}

	// Delete.
	m.cursor = m.store.Profiles.Find("new-one")
	m = press(t, m, "d", "k", "enter")
	if m.store.Profiles.Find("new-one") >= 0 || strings.Contains(readConfig(t, p), "new-one") {
		t.Fatal("profile not deleted")
	}
	if !strings.Contains(readConfig(t, p), `"gentle-orchestrator"`) {
		t.Fatal("gentle-ai agent lost")
	}
}

func TestAutoRegeneratesMissingAgentsOnStart(t *testing.T) {
	p := setup(t)
	if _, _, err := core.Import(p, core.ImportOptions{}); err != nil {
		t.Fatal(err)
	}
	m := New(p, fakeLoader)
	m = press(t, m, "a")
	// Simulate a gentle-ai sync that drops every profile agent.
	if err := os.WriteFile(p.OpencodeJSON(), []byte(strings.Replace(fixtureConfig, `,
    "odd-orchestrator-base": {"description": "ODD Orchestrator (base profile) [oc-profiles]", "mode": "primary", "model": "openai/gpt-x", "prompt": "x"}`, "", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	m = New(p, fakeLoader)
	if !strings.Contains(m.status, "Restored 4 missing profile agents") || m.errMsg != "" {
		t.Fatalf("status = %q err = %q", m.status, m.errMsg)
	}
	if !strings.Contains(readConfig(t, p), `"odd-orchestrator-base"`) {
		t.Fatal("agents not regenerated")
	}
	if strings.Contains(m.View(), "missing from opencode.json") {
		t.Fatal("banner shown after automatic recovery")
	}
}

func TestSyncKeyAndBackupRestoreFlow(t *testing.T) {
	p := setup(t)
	if _, _, err := core.Import(p, core.ImportOptions{}); err != nil {
		t.Fatal(err)
	}
	m := New(p, fakeLoader)
	if !strings.Contains(m.View(), "s: sync") || !strings.Contains(m.View(), "b: backups") {
		t.Fatal("help line missing sync/backups")
	}
	m = press(t, m, "s")
	if !strings.Contains(m.status, "Synced:") || !strings.Contains(m.status, "Backup #1") || m.errMsg != "" {
		t.Fatalf("sync status = %q err = %q", m.status, m.errMsg)
	}
	// Second sync: nothing changes, snapshot unchanged.
	m = press(t, m, "s")
	if !strings.Contains(m.status, "already in sync") || !strings.Contains(m.status, "Backup #1") || !strings.Contains(m.status, "unchanged") {
		t.Fatalf("second sync status = %q", m.status)
	}

	// Change the store (rename) -> snapshot #2, then restore #1 via the TUI.
	m.cursor = m.store.Profiles.Find("base")
	m = press(t, m, "r", "-", "x", "enter", "enter")
	if m.store.Profiles.Find("base-x") < 0 || !strings.Contains(m.status, "Backup #2") {
		t.Fatalf("rename status = %q err = %q", m.status, m.errMsg)
	}
	m = press(t, m, "b")
	if m.screen != screenBackups || len(m.backups) != 2 || m.backups[0].Seq != 2 {
		t.Fatalf("backups = %+v", m.backups)
	}
	m = press(t, m, "j", "enter") // snapshot #1
	if m.screen != screenRestoreConfirm || m.cursor != 1 {
		t.Fatal("restore must ask for confirmation, defaulting to Cancel")
	}
	m = press(t, m, "k", "enter")
	if m.errMsg != "" || m.store.Profiles.Find("base") < 0 || m.store.Profiles.Find("base-x") >= 0 {
		t.Fatalf("restore failed: %q %v", m.errMsg, m.store.Profiles.Names())
	}
	cfg := readConfig(t, p)
	if !strings.Contains(cfg, `"odd-orchestrator-base"`) || strings.Contains(cfg, "base-x") {
		t.Fatal("opencode.json not synced after restore")
	}
}

func TestMigrateScreenForV1Store(t *testing.T) {
	p := setup(t)
	v1 := `{"version": 1, "profiles": [{"name": "old", "orchestrator": {"model": "openai/gpt-x"}, "phases": {"sdd-apply": {"model": "zai/glm"}}}]}`
	if err := os.MkdirAll(p.StoreDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.ProfilesFile(), []byte(v1), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New(p, fakeLoader)
	if m.screen != screenMigrate || !strings.Contains(m.View(), "Migrate SDD profiles to ODD") {
		t.Fatalf("screen = %v", m.screen)
	}
	m = press(t, m, "enter")
	if m.screen != screenList || m.store.Profiles.Profiles[0].Get(core.RoleWorker).Model != "zai/glm" {
		t.Fatalf("migration failed: %s", m.errMsg)
	}
	if !strings.Contains(m.View(), "pending removal") {
		t.Fatal("cleanup banner missing")
	}
}

func TestNameValidation(t *testing.T) {
	p := setup(t)
	if _, _, err := core.Import(p, core.ImportOptions{}); err != nil {
		t.Fatal(err)
	}
	m := New(p, fakeLoader)
	m = press(t, m, "n", "b", "a", "s", "e", "enter")
	if m.screen != screenName || !strings.Contains(m.nameErr, "already exists") {
		t.Fatalf("duplicate name accepted: %v %q", m.screen, m.nameErr)
	}
	m = press(t, m, "backspace", "backspace", "backspace", "backspace", "-", "enter")
	if m.nameErr == "" {
		t.Fatal("invalid slug accepted")
	}
}

func TestRenameFlow(t *testing.T) {
	p := setup(t)
	if _, _, err := core.Import(p, core.ImportOptions{}); err != nil {
		t.Fatal(err)
	}
	m := New(p, fakeLoader)
	m = press(t, m, "a") // materialize odd-*-base
	if !strings.Contains(m.View(), "r: rename") {
		t.Fatal("help line missing rename")
	}

	m = press(t, m, "r")
	if m.screen != screenRename || string(m.nameInput) != "base" {
		t.Fatalf("rename input not prefilled: %v %q", m.screen, string(m.nameInput))
	}
	// Same name is a no-op.
	m = press(t, m, "enter")
	if m.screen != screenList || !strings.Contains(m.status, "unchanged") {
		t.Fatalf("same-name rename: %v %q", m.screen, m.status)
	}
	// Invalid slug refused.
	m = press(t, m, "r", "_", "enter")
	if m.screen != screenRename || m.nameErr == "" {
		t.Fatal("invalid name accepted")
	}
	// Valid rename: base -> base-two, then confirm.
	m = press(t, m, "backspace", "-", "t", "w", "o", "enter")
	if m.screen != screenRenameConfirm || !strings.Contains(m.View(), "odd-orchestrator-base → odd-orchestrator-base-two") {
		t.Fatalf("confirm screen wrong: %v %s", m.screen, m.nameErr)
	}
	m = press(t, m, "enter")
	if m.errMsg != "" || m.store.Profiles.Find("base-two") < 0 {
		t.Fatalf("rename failed: %s", m.errMsg)
	}
	cfg := readConfig(t, p)
	if strings.Contains(cfg, `"odd-orchestrator-base"`) || !strings.Contains(cfg, `"odd-orchestrator-base-two"`) {
		t.Fatal("opencode.json not updated")
	}
	if got := m.store.Profiles.Profiles[0].Get(core.RoleOrchestrator).Model; got != "openai/gpt-x" {
		t.Fatalf("model lost: %q", got)
	}

	// Duplicate refused: create a second profile then try to rename onto it.
	m.store.Profiles.Upsert(core.Profile{Name: "other", Models: map[string]core.Assignment{}})
	m.cursor = m.store.Profiles.Find("base-two")
	m = press(t, m, "r")
	m.nameInput, m.namePos = []rune("other"), 5
	m = press(t, m, "enter")
	if m.screen != screenRename || !strings.Contains(m.nameErr, "already exists") {
		t.Fatalf("duplicate accepted: %v %q", m.screen, m.nameErr)
	}
	// Cancel from confirm keeps the name.
	m.nameInput, m.namePos = []rune("zed"), 3
	m = press(t, m, "enter", "j", "enter")
	if m.store.Profiles.Find("base-two") < 0 {
		t.Fatal("cancel renamed the profile")
	}
}

func TestEditKeyAlias(t *testing.T) {
	p := setup(t)
	if _, _, err := core.Import(p, core.ImportOptions{}); err != nil {
		t.Fatal(err)
	}
	m := New(p, fakeLoader)
	m = press(t, m, "e")
	if m.screen != screenName || !m.editMode || m.draft.Name != "base" {
		t.Fatalf("e did not open edit: %v %v %q", m.screen, m.editMode, m.draft.Name)
	}
	if !strings.Contains(press(t, m, "esc").View(), "enter/e: edit") {
		t.Fatal("help line missing e")
	}
}
