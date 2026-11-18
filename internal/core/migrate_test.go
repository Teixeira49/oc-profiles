package core

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teixeira49/oc-profiles/internal/ojson"
)

// setupV1 writes a gentle-ai 3.7-era opencode.json, SDD asset files and a
// v1 oc-profiles store for profile "alpha".
func setupV1(t *testing.T) Paths {
	t.Helper()
	p := newPaths(t)
	agents := gentleAgents()
	managed := func(desc string) *ojson.Object {
		return obj("__managed_by", "gentle-ai/sdd", "description", desc, "hidden", true, "mode", "subagent", "prompt", desc)
	}
	agents.Set("jd-judge-a-alpha", obj("description", "Adversarial code reviewer — blind judge A", "hidden", true, "mode", "subagent",
		"model", "openai/gpt-judge", "prompt", "old 3.7 judge", "variant", "low"))
	agents.Set("explore", managed("Freeform exploration fallback agent"))
	agents.Set("general", managed("General fallback"))
	for _, ph := range legacySDDPhases {
		agents.Set(ph, managed("base "+ph))
		agents.Set(ph+"-alpha", obj("description", "phase", "hidden", true, "mode", "subagent", "model", "x/"+ph, "prompt", "{file:./prompts/sdd/"+ph+".md}"))
	}
	agents.Set("sdd-orchestrator-alpha", obj("description", "SDD Orchestrator (alpha profile)", "mode", "primary", "model", "x/orch", "prompt", "old", "variant", "high"))
	writeDoc(t, p, wrapDoc(agents))

	assets := map[string]string{
		"prompts/sdd/sdd-apply.md":              "apply prompt",
		"prompts/sdd/sdd-verify.md":             "verify prompt",
		"skills/sdd-apply/SKILL.md":             "skill",
		"skills/sdd-apply/references/a.md":      "ref",
		"skills/_shared/sdd-phase-common.md":    "common",
		"skills/_shared/openspec-convention.md": "openspec",
	}
	for rel, content := range assets {
		writeTestFile(t, filepath.Join(p.OpencodeDir, rel), []byte(content))
		writeTestFile(t, filepath.Join(p.StoreDir, "assets", rel), []byte(content))
	}
	// The user edited one file after the snapshot, and has unrelated files.
	writeTestFile(t, filepath.Join(p.OpencodeDir, "prompts/sdd/sdd-verify.md"), []byte("user edited"))
	writeTestFile(t, filepath.Join(p.OpencodeDir, "skills/_shared/engram-convention.md"), []byte("keep"))
	writeTestFile(t, filepath.Join(p.OpencodeDir, "skills/judgment-day/SKILL.md"), []byte("keep"))

	v1 := map[string]any{"version": 1, "profiles": []any{map[string]any{
		"name":         "alpha",
		"orchestrator": map[string]string{"model": "x/orch", "variant": "high"},
		"phases": map[string]any{
			"sdd-explore": map[string]string{"model": "x/explore"},
			"sdd-apply":   map[string]string{"model": "x/apply", "variant": "xhigh"},
			"sdd-verify":  map[string]string{"model": "x/verify", "variant": "medium"},
			"sdd-spec":    map[string]string{"model": "x/spec"},
			"jd-judge-a":  map[string]string{"model": "openai/gpt-judge", "variant": "low"},
		},
	}}}
	data, _ := json.MarshalIndent(v1, "", "  ")
	writeTestFile(t, p.ProfilesFile(), data)
	base := ojson.NewObject()
	_, live, _, _ := ReadOpencode(p)
	for _, k := range []string{"explore", "general", "sdd-apply"} {
		v, _ := live.Get(k)
		base.Set(k, v)
	}
	// v1 profile templates, as `import` (v1) wrote them: model/variant
	// dropped, profile name replaced by the placeholder.
	profileAgents := ojson.NewObject()
	profileAgents.Set("sdd-orchestrator", obj("description", "SDD Orchestrator ({{PROFILE}} profile)", "mode", "primary", "prompt", "old"))
	for _, ph := range legacySDDPhases {
		profileAgents.Set(ph, obj("description", "phase", "hidden", true, "mode", "subagent", "prompt", "{file:./prompts/sdd/"+ph+".md}"))
	}
	profileAgents.Set("jd-judge-a", obj("description", "Adversarial code reviewer — blind judge A", "hidden", true, "mode", "subagent", "prompt", "old 3.7 judge"))
	tdoc := obj("version", json.Number("1"), "profileAgents", profileAgents, "baseAgents", base)
	tdata, _ := ojson.Marshal(tdoc)
	writeTestFile(t, p.TemplatesFile(), tdata)
	return p
}

func TestMigrateMapsSDDToODD(t *testing.T) {
	p := setupV1(t)
	if _, err := LoadStore(p); err != ErrLegacyStore {
		t.Fatalf("want ErrLegacyStore, got %v", err)
	}
	res, err := Migrate(p)
	if err != nil {
		t.Fatal(err)
	}
	if !fileExists(filepath.Join(res.Backup, "profiles.json")) || !fileExists(filepath.Join(res.Backup, "assets", "prompts", "sdd", "sdd-apply.md")) {
		t.Fatalf("backup incomplete: %s", res.Backup)
	}
	s, err := LoadStore(p)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Profiles.SDDCleanup || len(s.Profiles.Profiles) != 1 {
		t.Fatalf("profiles = %+v", s.Profiles)
	}
	got := s.Profiles.Profiles[0]
	want := map[string]Assignment{
		RoleOrchestrator: {Model: "x/orch", Variant: "high"},
		RoleExplore:      {Model: "x/explore"},
		RoleWorker:       {Model: "x/apply", Variant: "xhigh"},
		RoleVerify:       {Model: "x/verify", Variant: "medium"},
		RoleJudgeA:       {Model: "openai/gpt-judge", Variant: "low"},
	}
	if mustJSON(t, got.Models) != mustJSON(t, want) {
		t.Fatalf("models = %+v", got.Models)
	}
	if got.Get(RoleFixAgent).IsSet() {
		t.Fatal("migration must leave the fix agent unassigned")
	}
	if _, err := Migrate(p); err == nil {
		t.Fatal("second migrate must refuse")
	}
}

func TestApplyAfterMigrateCleansUpSDD(t *testing.T) {
	p := setupV1(t)
	if _, err := Migrate(p); err != nil {
		t.Fatal(err)
	}
	s, _ := LoadStore(p)

	plan, err := s.Plan()
	if err != nil {
		t.Fatal(err)
	}
	if !plan.CleanupPending || plan.InSync() {
		t.Fatal("plan must report pending cleanup")
	}
	if !fileExists(filepath.Join(p.OpencodeDir, "prompts/sdd/sdd-apply.md")) {
		t.Fatal("plan must not delete files")
	}

	r, err := s.Apply()
	if err != nil {
		t.Fatal(err)
	}
	agents := agentsOf(t, readTestFile(t, p.OpencodeJSON()))
	for _, k := range agents.Keys() {
		if strings.HasPrefix(k, "sdd-") || k == "explore" || k == "general" {
			t.Fatalf("legacy agent %s survived", k)
		}
	}
	for _, k := range []string{"odd-orchestrator-alpha", "odd-explore-alpha", "odd-worker-alpha", "odd-verify-alpha", "jd-judge-a-alpha"} {
		if !agents.Has(k) {
			t.Fatalf("%s missing", k)
		}
	}
	j, _ := agents.GetObject("jd-judge-a-alpha")
	if d, _ := j.GetString("description"); d != "Judgment Day blind adversarial reviewer A. (alpha profile) [oc-profiles]" {
		t.Fatalf("3.7 judge not regenerated from live template: %q", d)
	}
	if pr, _ := j.GetString("prompt"); pr != "You are judge A." {
		t.Fatal("judge prompt not from live template")
	}

	wantRemoved := []string{
		"prompts/sdd/sdd-apply.md", "skills/_shared/openspec-convention.md", "skills/_shared/sdd-phase-common.md",
		"skills/sdd-apply/SKILL.md", "skills/sdd-apply/references/a.md",
	}
	if strings.Join(r.RemovedFiles, ",") != strings.Join(wantRemoved, ",") {
		t.Fatalf("removed files = %v", r.RemovedFiles)
	}
	if !strings.Contains(strings.Join(r.Warnings, "\n"), "kept modified SDD file prompts/sdd/sdd-verify.md") {
		t.Fatalf("warnings = %v", r.Warnings)
	}
	if dirExists(filepath.Join(p.OpencodeDir, "skills/sdd-apply")) {
		t.Fatal("empty skill dir not removed")
	}
	for _, keep := range []string{"prompts/sdd/sdd-verify.md", "skills/_shared/engram-convention.md", "skills/judgment-day/SKILL.md"} {
		if !fileExists(filepath.Join(p.OpencodeDir, keep)) {
			t.Fatalf("%s must be kept", keep)
		}
	}

	// Cleanup is one-time: flag cleared, legacy snapshot gone, next apply no-op.
	s2, _ := LoadStore(p)
	if s2.Profiles.SDDCleanup || dirExists(p.LegacyDir()) {
		t.Fatal("cleanup state not cleared")
	}
	before := readTestFile(t, p.OpencodeJSON())
	r2, err := s2.Apply()
	if err != nil || !r2.InSync() {
		t.Fatalf("second apply: %+v %v", r2, err)
	}
	if !bytes.Equal(before, readTestFile(t, p.OpencodeJSON())) {
		t.Fatal("second apply changed the file")
	}
	// gentle-ai's own agents are untouched.
	orig := gentleAgents()
	for _, k := range orig.Keys() {
		ov, _ := orig.Get(k)
		av, _ := agents.Get(k)
		if mustJSON(t, ov) != mustJSON(t, av) {
			t.Fatalf("%s changed", k)
		}
	}
}

func TestMigrateKeepsCustomizedExplore(t *testing.T) {
	p := setupV1(t)
	doc, _ := ojson.ParseObject(readTestFile(t, p.OpencodeJSON()))
	agents, _ := doc.GetObject("agent")
	agents.Set("general", obj("description", "my own general", "mode", "subagent"))
	writeDoc(t, p, doc)
	if _, err := Migrate(p); err != nil {
		t.Fatal(err)
	}
	s, _ := LoadStore(p)
	r, err := s.Apply()
	if err != nil {
		t.Fatal(err)
	}
	if !agentsOf(t, readTestFile(t, p.OpencodeJSON())).Has("general") {
		t.Fatal("customized general removed")
	}
	if !strings.Contains(strings.Join(r.Warnings, "\n"), `kept agent "general"`) {
		t.Fatalf("warnings = %v", r.Warnings)
	}
}
