package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Teixeira49/oc-profiles/internal/ojson"
)

// --- 1. symlinks -------------------------------------------------------------

func TestApplyWritesThroughSymlinkedOpencodeJSON(t *testing.T) {
	p := newPaths(t)
	writeDoc(t, p, wrapDoc(gentleAgents()))
	// Move the real file into a "dotfiles" dir and symlink it back.
	dotfiles := filepath.Join(filepath.Dir(p.OpencodeDir), "dotfiles")
	if err := os.MkdirAll(dotfiles, 0o755); err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(dotfiles, "opencode.json")
	if err := os.Rename(p.OpencodeJSON(), real); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(real, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, p.OpencodeJSON()); err != nil {
		t.Fatal(err)
	}
	// Also symlink profiles.json.
	s := newStore(t, p, prof("alpha", map[string]Assignment{RoleOrchestrator: opus}))
	realProfiles := filepath.Join(dotfiles, "profiles.json")
	if err := os.Rename(p.ProfilesFile(), realProfiles); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realProfiles, p.ProfilesFile()); err != nil {
		t.Fatal(err)
	}

	if _, err := s.Apply(); err != nil {
		t.Fatal(err)
	}
	s.Profiles.Upsert(prof("beta", nil))
	if err := s.SaveProfiles(); err != nil {
		t.Fatal(err)
	}
	for link, target := range map[string]string{p.OpencodeJSON(): real, p.ProfilesFile(): realProfiles} {
		info, err := os.Lstat(link)
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("%s is no longer a symlink", link)
		}
		if got, _ := os.Readlink(link); got != target {
			t.Fatalf("%s points to %s", link, got)
		}
	}
	if !agentsOf(t, readTestFile(t, real)).Has("odd-orchestrator-alpha") {
		t.Fatal("symlink target not updated")
	}
	if !bytes.Contains(readTestFile(t, realProfiles), []byte(`"beta"`)) {
		t.Fatal("profiles symlink target not updated")
	}
	if info, _ := os.Stat(real); info.Mode().Perm() != 0o640 {
		t.Fatalf("target mode changed to %v", info.Mode().Perm())
	}
	if m, _ := filepath.Glob(filepath.Join(dotfiles, ".*.tmp-*")); len(m) != 0 {
		t.Fatalf("temp files left: %v", m)
	}
}

// --- 2. ownership --------------------------------------------------------------

func TestApplyNeverTouchesUnmarkedAgents(t *testing.T) {
	p := newPaths(t)
	agents := gentleAgents()
	agents.Set("odd-explore-mine", obj("description", "my own explorer", "mode", "subagent"))
	agents.Set("jd-fix-agent-custom", obj("description", "my own fixer", "mode", "subagent"))
	writeDoc(t, p, wrapDoc(agents))
	s := newStore(t, p, prof("alpha", map[string]Assignment{RoleOrchestrator: opus}))
	r, err := s.Apply()
	if err != nil {
		t.Fatal(err)
	}
	a := agentsOf(t, readTestFile(t, p.OpencodeJSON()))
	for _, k := range []string{"odd-explore-mine", "jd-fix-agent-custom"} {
		v, _ := a.Get(k)
		orig, _ := agents.Get(k)
		if mustJSON(t, v) != mustJSON(t, orig) {
			t.Fatalf("%s was modified or removed", k)
		}
	}
	if w := strings.Join(r.Warnings, "\n"); !strings.Contains(w, `kept "odd-explore-mine"`) || !strings.Contains(w, `kept "jd-fix-agent-custom"`) {
		t.Fatalf("warnings = %v", r.Warnings)
	}

	// A profile that would overwrite an unmarked agent is refused, naming it.
	before := readTestFile(t, p.OpencodeJSON())
	s.Profiles.Upsert(prof("custom", map[string]Assignment{RoleFixAgent: fable}))
	_, err = s.Apply()
	if err == nil || !strings.Contains(err.Error(), `"jd-fix-agent-custom" already exists and is not managed by oc-profiles`) {
		t.Fatalf("err = %v", err)
	}
	if !bytes.Equal(before, readTestFile(t, p.OpencodeJSON())) {
		t.Fatal("file written despite the ownership conflict")
	}
}

// --- 3. legacy cleanup guard -------------------------------------------------------

func TestLegacyCleanupKeepsCustomizedSDDAgents(t *testing.T) {
	p := setupV1(t)
	doc, _ := ojson.ParseObject(readTestFile(t, p.OpencodeJSON()))
	agents, _ := doc.GetObject("agent")
	agents.Set("sdd-spec-alpha", obj("description", "phase", "hidden", true, "mode", "subagent", "prompt", "my custom spec prompt"))
	agents.Set("sdd-design-mine", obj("description", "unrelated user agent", "mode", "subagent"))
	agents.Set("sdd-tasks-other", obj("__managed_by", "gentle-ai/sdd", "description", "gentle output", "mode", "subagent"))
	writeDoc(t, p, doc)
	if _, err := Migrate(p); err != nil {
		t.Fatal(err)
	}
	s, _ := LoadStore(p)
	r, err := s.Apply()
	if err != nil {
		t.Fatal(err)
	}
	a := agentsOf(t, readTestFile(t, p.OpencodeJSON()))
	if !a.Has("sdd-spec-alpha") || !a.Has("sdd-design-mine") {
		t.Fatal("customized/unrelated SDD agents removed")
	}
	if a.Has("sdd-tasks-other") || a.Has("sdd-apply-alpha") || a.Has("sdd-apply") || a.Has("sdd-orchestrator-alpha") {
		t.Fatal("gentle-ai/v1 SDD agents not removed")
	}
	w := strings.Join(r.Warnings, "\n")
	if !strings.Contains(w, `kept agent "sdd-spec-alpha"`) || !strings.Contains(w, `kept agent "sdd-design-mine"`) {
		t.Fatalf("warnings = %v", r.Warnings)
	}
}

// --- 4. reserved / invalid / duplicate names -----------------------------------

func TestDetectSkipsReservedNamesAndLoadRejectsBadStores(t *testing.T) {
	agents := gentleAgents()
	agents.Set("odd-orchestrator-default", obj("description", "x [oc-profiles]", "mode", "primary"))
	agents.Set("odd-orchestrator-ok", obj("description", "x [oc-profiles]", "mode", "primary"))
	ps, warnings := DetectODDProfiles(agents)
	if strings.Join(ps.Names(), ",") != "ok" || len(warnings) != 1 || !strings.Contains(warnings[0], "odd-orchestrator-default") {
		t.Fatalf("names %v warnings %v", ps.Names(), warnings)
	}

	p := newPaths(t)
	for name, body := range map[string]string{
		"duplicate": `{"version": 2, "profiles": [{"name": "a", "models": {}}, {"name": "a", "models": {}}]}`,
		"reserved":  `{"version": 2, "profiles": [{"name": "default", "models": {}}]}`,
		"invalid":   `{"version": 2, "profiles": [{"name": "Bad_Name", "models": {}}]}`,
	} {
		writeTestFile(t, p.ProfilesFile(), []byte(body))
		if _, err := LoadStore(p); err == nil {
			t.Fatalf("%s store accepted", name)
		}
	}
}

// --- 5. newer schema -----------------------------------------------------------------

func TestNewerStoreIsTyped(t *testing.T) {
	p := newPaths(t)
	writeTestFile(t, p.ProfilesFile(), []byte(`{"version": 3, "profiles": []}`))
	if _, err := LoadStore(p); !errors.Is(err, ErrNewerStore) {
		t.Fatalf("err = %v", err)
	}
	if _, err := ParseStorePayload(p, []byte(`{"version": 9, "profiles": []}`), nil); !errors.Is(err, ErrNewerStore) {
		t.Fatalf("payload err = %v", err)
	}
}

// --- 6. path-context tokens -------------------------------------------------------------

func TestReplaceBoundedSkipsPathContexts(t *testing.T) {
	cases := map[string]string{
		// rewritten
		"use gentle-ai-worker now":      "use W now",
		"`gentle-ai-worker`":            "`W`",
		"ask gentle-ai-worker.":         "ask W.",
		"ask gentle-ai-worker.\nnext":   "ask W.\nnext",
		"(gentle-ai-worker)":            "(W)",
		"\"gentle-ai-worker\"":          "\"W\"",
		"gentle-ai-worker":              "W",
		"agent.gentle-ai-worker.model":  "agent.W.model",
		"gentle-ai-worker, then verify": "W, then verify",
		// path / file-reference contexts: untouched
		"prompts/gentle-ai-worker.md":          "prompts/gentle-ai-worker.md",
		"skills/gentle-ai-worker/SKILL.md":     "skills/gentle-ai-worker/SKILL.md",
		"{file:./prompts/gentle-ai-worker.md}": "{file:./prompts/gentle-ai-worker.md}",
		"{file:gentle-ai-worker.md}":           "{file:W.md}" + "",
		"see gentle-ai-worker.md":              "see gentle-ai-worker.md",
		"gentle-ai-worker: does the writing":   "gentle-ai-worker: does the writing",
		"gentle-ai-worker/notes":               "gentle-ai-worker/notes",
		"x/gentle-ai-worker":                   "x/gentle-ai-worker",
		// not bounded
		"gentle-ai-workers my-gentle-ai-worker": "gentle-ai-workers my-gentle-ai-worker",
	}
	// "{file:gentle-ai-worker.md}" is followed by ".md": untouched too.
	cases["{file:gentle-ai-worker.md}"] = "{file:gentle-ai-worker.md}"
	for in, want := range cases {
		if got := ReplaceBounded(in, "gentle-ai-worker", "W"); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
	// Permission keys are whole strings and still rewritten by RenderAgent.
	src := obj("permission", obj("task", obj("gentle-ai-worker", "allow")), "prompt", "Read prompts/gentle-ai-worker.md, then delegate to `gentle-ai-worker`.")
	r, _ := RoleByID(RoleOrchestrator)
	got := RenderAgent(r, src, prof("p", nil))
	js := mustJSON(t, got)
	if !strings.Contains(js, `"odd-worker-p": "allow"`) || !strings.Contains(js, "Read prompts/gentle-ai-worker.md, then delegate to `odd-worker-p`.") {
		t.Fatalf("render = %s", js)
	}
	if err := Validate(objWith(got), Profiles{Profiles: []Profile{prof("p", nil)}}); err != nil && strings.Contains(err.Error(), "still references") {
		t.Fatalf("path reference treated as leftover: %v", err)
	}
}

func objWith(orch *ojson.Object) *ojson.Object {
	a := ojson.NewObject()
	a.Set("odd-orchestrator-p", orch)
	return a
}

// --- 8. migrate resumability -------------------------------------------------------------

func TestMigrateIsResumableAndLegacyDirCleanupIsScoped(t *testing.T) {
	p := setupV1(t)
	// A previous, interrupted migrate already copied the assets snapshot.
	writeTestFile(t, filepath.Join(p.LegacyDir(), "assets", "prompts", "sdd", "sdd-apply.md"), []byte("apply prompt"))
	// The user keeps a note in legacy/.
	writeTestFile(t, filepath.Join(p.LegacyDir(), "notes.txt"), []byte("mine"))
	if _, err := Migrate(p); err != nil {
		t.Fatal(err)
	}
	if fileExists(filepath.Join(p.LegacyDir(), "assets", "skills", "sdd-apply", "SKILL.md")) {
		t.Fatal("existing legacy/assets was overwritten")
	}
	if dirExists(filepath.Join(p.StoreDir, "assets")) {
		t.Fatal("v1 assets dir not removed after a successful save")
	}
	s, _ := LoadStore(p)
	if _, err := s.Apply(); err != nil {
		t.Fatal(err)
	}
	if dirExists(p.legacyAssetsDir()) || fileExists(p.legacyTemplatesFile()) {
		t.Fatal("known legacy children not removed")
	}
	if !fileExists(filepath.Join(p.LegacyDir(), "notes.txt")) {
		t.Fatal("user file in legacy/ deleted")
	}
}

// --- 10. import --force -------------------------------------------------------------------

func TestImportForceMovesAsideAndRefusesEmpty(t *testing.T) {
	p := newPaths(t)
	writeDoc(t, p, wrapDoc(gentleAgents())) // no ODD profiles in opencode.json
	newStore(t, p, prof("alpha", map[string]Assignment{RoleOrchestrator: opus}))
	before := readTestFile(t, p.ProfilesFile())

	if _, _, err := Import(p, ImportOptions{Force: true}); !errors.Is(err, ErrImportWouldEmpty) {
		t.Fatalf("err = %v", err)
	}
	if !bytes.Equal(before, readTestFile(t, p.ProfilesFile())) {
		t.Fatal("store touched by a refused import")
	}
	_, res, err := Import(p, ImportOptions{Force: true, AllowEmpty: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.MovedAside) != 2 || !strings.Contains(res.MovedAside[0], ".replaced-") {
		t.Fatalf("moved = %v", res.MovedAside)
	}
	if !bytes.Equal(before, readTestFile(t, res.MovedAside[0])) {
		t.Fatal("old store not preserved")
	}
}

// --- 11. concurrency guard -----------------------------------------------------------------

func TestApplyAbortsWhenFileChangesConcurrently(t *testing.T) {
	p := newPaths(t)
	writeDoc(t, p, wrapDoc(gentleAgents()))
	s := newStore(t, p, prof("alpha", map[string]Assignment{RoleOrchestrator: opus}))
	other := []byte(`{"agent": {}}`)
	testHookBeforeWrite = func() { writeTestFile(t, p.OpencodeJSON(), other) }
	t.Cleanup(func() { testHookBeforeWrite = nil })
	_, err := s.Apply()
	if !errors.Is(err, ErrConcurrentChange) || !strings.Contains(err.Error(), "opencode.json changed during apply; re-run") {
		t.Fatalf("err = %v", err)
	}
	if !bytes.Equal(readTestFile(t, p.OpencodeJSON()), other) {
		t.Fatal("concurrent writer's content was overwritten")
	}
}

// --- LOW --------------------------------------------------------------------------------

func TestOpencodeBackupsRotateToTen(t *testing.T) {
	p := newPaths(t)
	writeDoc(t, p, wrapDoc(gentleAgents()))
	s := newStore(t, p)
	for i := 0; i < 13; i++ {
		s.Profiles.Upsert(prof("p"+string(rune('a'+i)), nil))
		if _, err := s.Apply(); err != nil {
			t.Fatal(err)
		}
	}
	m, _ := filepath.Glob(p.OpencodeJSON() + ".bak-oc-profiles-*")
	if len(m) != MaxOpencodeBackups {
		t.Fatalf("%d backups kept", len(m))
	}
	// The newest backup is kept (all share one fixed timestamp; suffix -12 is newest).
	if !fileExists(p.OpencodeJSON() + ".bak-oc-profiles-20260102030405-12") {
		t.Fatalf("newest backup rotated away: %v", m)
	}
}

func TestCleanupOnlyApplyReportsStoreChange(t *testing.T) {
	p := setupV1(t)
	if _, err := Migrate(p); err != nil {
		t.Fatal(err)
	}
	s, _ := LoadStore(p)
	if _, err := s.Apply(); err != nil {
		t.Fatal(err)
	}
	// Simulate a cleanup flag set again with nothing left to clean.
	s.Profiles.SDDCleanup = true
	if err := s.SaveProfiles(); err != nil {
		t.Fatal(err)
	}
	r, err := s.Apply()
	if err != nil {
		t.Fatal(err)
	}
	if r.Changed || !r.StoreChanged || r.InSync() {
		t.Fatalf("cleanup-only apply: %+v", r)
	}
}

func TestAgentMapMustBeAnObject(t *testing.T) {
	p := newPaths(t)
	writeTestFile(t, p.OpencodeJSON(), []byte(`{"agent": null}`))
	if _, _, _, err := ReadOpencode(p); err == nil || !strings.Contains(err.Error(), `"agent" must be an object, found null`) {
		t.Fatalf("err = %v", err)
	}
	writeTestFile(t, p.OpencodeJSON(), []byte(`{"share": "disabled"}`))
	s := &Store{Paths: p, Profiles: Profiles{Profiles: []Profile{prof("a", nil)}}, Sources: ojson.NewObject()}
	if _, err := s.Plan(); err == nil || !strings.Contains(err.Error(), `has no "agent" section`) {
		t.Fatalf("err = %v", err)
	}
	_ = json.Valid
}
