package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teixeira49/oc-profiles/internal/ojson"
)

var (
	opus  = Assignment{Model: "anthropic/opus", Variant: "high"}
	gpt   = Assignment{Model: "openai/gpt-x", Variant: "xhigh"}
	fable = Assignment{Model: "anthropic/fable"}
)

func TestReplaceBoundedSafety(t *testing.T) {
	in := "`gentle-ai-explore` gentle-ai-explorer my-gentle-ai-explore gentle-ai-explore. gentle-ai-explore_x " +
		"agent.gentle-orchestrator.model gentle-orchestrator-legacy (gentle-ai-explore) gentle-ai-explore"
	got := ReplaceBounded(in, "gentle-ai-explore", "odd-explore-p")
	got = ReplaceBounded(got, "gentle-orchestrator", "odd-orchestrator-p")
	want := "`odd-explore-p` gentle-ai-explorer my-gentle-ai-explore odd-explore-p. gentle-ai-explore_x " +
		"agent.odd-orchestrator-p.model gentle-orchestrator-legacy (odd-explore-p) odd-explore-p"
	if got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
	if !ContainsBounded("x `gentle-ai-worker` y", "gentle-ai-worker") || ContainsBounded("gentle-ai-workers", "gentle-ai-worker") {
		t.Fatal("ContainsBounded wrong")
	}
}

func TestSplitProfileKeyAndLegacyKeys(t *testing.T) {
	for key, want := range map[string]string{
		"odd-orchestrator-chappie-ssj": "orchestrator/chappie-ssj",
		"odd-worker-a":                 "worker/a",
		"jd-judge-b-conde":             "jd-judge-b/conde",
	} {
		r, name, ok := SplitProfileKey(key)
		if !ok || r.ID+"/"+name != want {
			t.Errorf("%s -> %s/%s %v", key, r.ID, name, ok)
		}
	}
	for _, key := range []string{"jd-judge-a", "jd-fix-agent", "gentle-orchestrator", "gentle-ai-worker", "odd-orchestrator", "review-risk"} {
		if _, _, ok := SplitProfileKey(key); ok {
			t.Errorf("%s must not be a profile key", key)
		}
	}
	for _, key := range []string{"sdd-apply", "sdd-apply-chappie", "sdd-orchestrator-x"} {
		if !IsLegacySDDKey(key) {
			t.Errorf("%s should be legacy", key)
		}
	}
	for _, key := range []string{"sdd-orchestrator", "sdd-applyx", "jd-judge-a-x", "explore", "odd-explore-x"} {
		if IsLegacySDDKey(key) {
			t.Errorf("%s should not be legacy", key)
		}
	}
}

func TestRenderOrchestratorRewrites(t *testing.T) {
	src, _ := gentleAgents().GetObject("gentle-orchestrator")
	p := prof("pro", map[string]Assignment{RoleOrchestrator: gpt, RoleJudgeA: fable})
	r, _ := RoleByID(RoleOrchestrator)
	got := RenderAgent(r, src, p)

	if strings.Join(got.Keys(), ",") != "description,mode,model,permission,prompt,variant" {
		t.Fatalf("keys = %v", got.Keys())
	}
	if d, _ := got.GetString("description"); d != "ODD Orchestrator (pro profile) [oc-profiles]" {
		t.Fatalf("description = %q", d)
	}
	if m, _ := got.GetString("mode"); m != "primary" {
		t.Fatal("mode must be primary")
	}
	if m, _ := got.GetString("model"); m != "openai/gpt-x" {
		t.Fatal("model not applied")
	}
	prompt, _ := got.GetString("prompt")
	for _, want := range []string{
		"`odd-orchestrator-pro` agent only", "`odd-explore-pro`", "writing to odd-worker-pro", "\"odd-verify-pro\"",
		"agent.odd-orchestrator-pro.model", "gentle-ai-explorer", "my-gentle-ai-worker", "gentle-orchestrator-legacy",
		"Judges: jd-judge-a-pro, jd-judge-b, fixes: jd-fix-agent, review-risk.",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
	perm, _ := got.GetObject("permission")
	task, _ := perm.GetObject("task")
	if k := strings.Join(task.Keys(), ","); k != "*,odd-explore-pro,odd-verify-pro,odd-worker-pro,jd-fix-agent,jd-judge-a-pro,jd-judge-b,review-risk" {
		t.Fatalf("task keys = %s", k)
	}
	// The live source must not be mutated.
	if p2, _ := src.GetString("prompt"); p2 != orchestratorPromptFixture {
		t.Fatal("source mutated")
	}
}

func TestRenderSubagentDescription(t *testing.T) {
	src, _ := gentleAgents().GetObject("gentle-ai-worker")
	r, _ := RoleByID(RoleWorker)
	got := RenderAgent(r, src, prof("pro", nil))
	if d, _ := got.GetString("description"); d != "Scoped implementation writer. (pro profile) [oc-profiles]" {
		t.Fatalf("description = %q", d)
	}
	if got.Has("model") || got.Has("variant") {
		t.Fatal("unassigned role must not carry a model")
	}
	if h, _ := got.Get("hidden"); h != true {
		t.Fatal("subagent must stay hidden")
	}
}

func TestApplyIdempotentAndGentleAgentsUntouched(t *testing.T) {
	p := newPaths(t)
	orig := writeDoc(t, p, wrapDoc(gentleAgents()))
	s := newStore(t, p,
		prof("alpha", map[string]Assignment{RoleOrchestrator: opus, RoleWorker: gpt, RoleJudgeA: fable, RoleJudgeB: opus}),
		prof("alpha-beta", map[string]Assignment{RoleOrchestrator: gpt}))

	r1, err := s.Apply()
	if err != nil {
		t.Fatal(err)
	}
	if !r1.Changed || r1.Backup == "" || len(r1.Added) != 6+4 {
		t.Fatalf("first apply: %+v", r1)
	}
	if !bytes.Equal(readTestFile(t, r1.Backup), orig) {
		t.Fatal("backup is not the original file")
	}
	first := readTestFile(t, p.OpencodeJSON())

	r2, err := s.Apply()
	if err != nil {
		t.Fatal(err)
	}
	if !r2.InSync() || r2.Backup != "" {
		t.Fatalf("second apply not a no-op: %+v", r2)
	}
	if !bytes.Equal(first, readTestFile(t, p.OpencodeJSON())) {
		t.Fatal("second apply changed the file")
	}

	before, _ := ojson.ParseObject(orig)
	after, _ := ojson.ParseObject(first)
	if strings.Join(before.Keys(), ",") != strings.Join(after.Keys(), ",") {
		t.Fatal("top-level keys changed")
	}
	for _, k := range []string{"$schema", "default_agent", "mcp", "plugin", "share"} {
		bv, _ := before.Get(k)
		av, _ := after.Get(k)
		if mustJSON(t, bv) != mustJSON(t, av) {
			t.Fatalf("%s changed", k)
		}
	}
	ba, _ := before.GetObject("agent")
	aa, _ := after.GetObject("agent")
	for _, k := range ba.Keys() {
		bv, _ := ba.Get(k)
		av, _ := aa.Get(k)
		if mustJSON(t, bv) != mustJSON(t, av) {
			t.Fatalf("gentle-ai agent %s changed", k)
		}
	}
	// Judges only for alpha; alpha-beta's orchestrator points at global judges.
	if aa.Has("jd-judge-a-alpha-beta") || !aa.Has("jd-judge-b-alpha") {
		t.Fatal("judge generation wrong")
	}
	ab, _ := aa.GetObject("odd-orchestrator-alpha-beta")
	if !strings.Contains(mustJSON(t, ab), `"jd-judge-a": "allow"`) {
		t.Fatal("alpha-beta should delegate to the global judge")
	}
}

func TestLiveTemplatePropagation(t *testing.T) {
	p := newPaths(t)
	agents := gentleAgents()
	writeDoc(t, p, wrapDoc(agents))
	s := newStore(t, p, prof("alpha", map[string]Assignment{RoleOrchestrator: opus}))
	if _, err := s.Apply(); err != nil {
		t.Fatal(err)
	}

	// gentle-ai ships a new orchestrator prompt and worker description.
	doc, _ := ojson.ParseObject(readTestFile(t, p.OpencodeJSON()))
	live, _ := doc.GetObject("agent")
	orch, _ := live.GetObject("gentle-orchestrator")
	orch.Set("prompt", orchestratorPromptFixture+"\nNEW RULE: ask gentle-ai-verify before shipping.\n")
	worker, _ := live.GetObject("gentle-ai-worker")
	worker.Set("description", "Improved worker.")
	writeDoc(t, p, doc)

	r, err := s.Apply()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(r.Updated, ",") != "odd-orchestrator-alpha,odd-worker-alpha" {
		t.Fatalf("updated = %v", r.Updated)
	}
	a := agentsOf(t, readTestFile(t, p.OpencodeJSON()))
	o, _ := a.GetObject("odd-orchestrator-alpha")
	if pr, _ := o.GetString("prompt"); !strings.Contains(pr, "NEW RULE: ask odd-verify-alpha before shipping.") {
		t.Fatal("new upstream prompt not propagated")
	}
	w, _ := a.GetObject("odd-worker-alpha")
	if d, _ := w.GetString("description"); d != "Improved worker. (alpha profile) [oc-profiles]" {
		t.Fatalf("worker description = %q", d)
	}
	// The snapshot follows the live source.
	reloaded, _ := LoadStore(p)
	snap, _ := reloaded.Sources.GetObject("gentle-ai-worker")
	if d, _ := snap.GetString("description"); d != "Improved worker." {
		t.Fatal("template snapshot not refreshed")
	}
}

func TestSnapshotFallbackWhenGentleAgentMissing(t *testing.T) {
	p := newPaths(t)
	writeDoc(t, p, wrapDoc(gentleAgents()))
	s := newStore(t, p, prof("alpha", map[string]Assignment{RoleOrchestrator: opus}))
	if _, err := s.Apply(); err != nil {
		t.Fatal(err)
	}
	doc, _ := ojson.ParseObject(readTestFile(t, p.OpencodeJSON()))
	live, _ := doc.GetObject("agent")
	live.Delete("gentle-ai-explore")
	writeDoc(t, p, doc)

	r, err := s.Apply()
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Warnings) != 1 || !strings.Contains(r.Warnings[0], "gentle-ai-explore") {
		t.Fatalf("warnings = %v", r.Warnings)
	}
	if !agentsOf(t, readTestFile(t, p.OpencodeJSON())).Has("odd-explore-alpha") {
		t.Fatal("explore lost")
	}
}

func TestValidationGateRefusesAndWritesNothing(t *testing.T) {
	p := newPaths(t)
	agents := gentleAgents()
	orch, _ := agents.GetObject("gentle-orchestrator")
	perm, _ := orch.GetObject("permission")
	task, _ := perm.GetObject("task")
	task.Set("ghost-agent", "allow")
	orig := writeDoc(t, p, wrapDoc(agents))
	s := newStore(t, p, prof("alpha", map[string]Assignment{RoleOrchestrator: opus}))

	_, err := s.Apply()
	if err == nil || !strings.Contains(err.Error(), `task target "ghost-agent" does not exist`) {
		t.Fatalf("err = %v", err)
	}
	if !bytes.Equal(readTestFile(t, p.OpencodeJSON()), orig) {
		t.Fatal("file written despite validation failure")
	}
	if m, _ := filepath.Glob(p.OpencodeJSON() + ".bak-*"); len(m) != 0 {
		t.Fatal("backup written despite validation failure")
	}

	// Leftover gentle-ai tokens in a profile orchestrator prompt are refused.
	bad := ojson.NewObject()
	for _, k := range []string{"odd-explore-x", "odd-worker-x", "odd-verify-x"} {
		bad.Set(k, obj("mode", "subagent"))
	}
	bad.Set("odd-orchestrator-x", obj("prompt", "use `gentle-ai-worker` now"))
	err = Validate(bad, Profiles{Profiles: []Profile{prof("x", nil)}})
	if err == nil || !strings.Contains(err.Error(), `still references "gentle-ai-worker"`) {
		t.Fatalf("err = %v", err)
	}
}

func TestApplyRemovesDeletedProfilesAndJudges(t *testing.T) {
	p := newPaths(t)
	writeDoc(t, p, wrapDoc(gentleAgents()))
	s := newStore(t, p,
		prof("alpha", map[string]Assignment{RoleJudgeA: fable}),
		prof("beta", map[string]Assignment{RoleOrchestrator: opus}))
	if _, err := s.Apply(); err != nil {
		t.Fatal(err)
	}
	s.Profiles.Remove("beta")
	a := s.Profiles.Profiles[0]
	a.Set(RoleJudgeA, Assignment{})
	s.Profiles.Upsert(a)
	r, err := s.Apply()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(r.Removed, ","); got != "jd-judge-a-alpha,odd-explore-beta,odd-orchestrator-beta,odd-verify-beta,odd-worker-beta" {
		t.Fatalf("removed = %s", got)
	}
	agents := agentsOf(t, readTestFile(t, p.OpencodeJSON()))
	if !agents.Has("jd-judge-a") || !agents.Has("odd-orchestrator-alpha") {
		t.Fatal("kept agents missing")
	}
}

func TestImportReadsODDProfiles(t *testing.T) {
	p := newPaths(t)
	writeDoc(t, p, wrapDoc(gentleAgents()))
	s := newStore(t, p, prof("alpha", map[string]Assignment{RoleOrchestrator: opus, RoleVerify: gpt, RoleJudgeB: fable}))
	if _, err := s.Apply(); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(p.StoreDir); err != nil {
		t.Fatal(err)
	}
	got, res, err := Import(p, ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.FromSDD || len(res.MissingSources) != 0 {
		t.Fatalf("res = %+v", res)
	}
	if mustJSON(t, got.Profiles.Profiles[0].Models) != mustJSON(t, s.Profiles.Profiles[0].Models) {
		t.Fatalf("models = %+v", got.Profiles.Profiles[0].Models)
	}
	if _, _, err := Import(p, ImportOptions{}); !errors.Is(err, ErrStoreExists) {
		t.Fatalf("want ErrStoreExists, got %v", err)
	}
}

func TestValidateProfileName(t *testing.T) {
	for _, ok := range []string{"a", "chappie", "conde-claudio-alberto-ssj2"} {
		if err := ValidateProfileName(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "-a", "a-", "A", "a_b", "default", "jd-judge-a"} {
		if ValidateProfileName(bad) == nil {
			t.Errorf("%q should be invalid", bad)
		}
	}
}

func TestFixAgentRewriteAndBoundaries(t *testing.T) {
	agents := gentleAgents()
	orch, _ := agents.GetObject("gentle-orchestrator")
	orch.Set("prompt", "Fixes go to `jd-fix-agent`; never jd-fix-agents, jd-fix-agent-legacy or my-jd-fix-agent. Use jd-fix-agent.")
	r, _ := RoleByID(RoleOrchestrator)
	got := RenderAgent(r, orch, prof("pro", map[string]Assignment{RoleFixAgent: fable}))
	prompt, _ := got.GetString("prompt")
	want := "Fixes go to `jd-fix-agent-pro`; never jd-fix-agents, jd-fix-agent-legacy or my-jd-fix-agent. Use jd-fix-agent-pro."
	if prompt != want {
		t.Fatalf("\n got %s\nwant %s", prompt, want)
	}
	perm, _ := got.GetObject("permission")
	task, _ := perm.GetObject("task")
	if !task.Has("jd-fix-agent-pro") || task.Has("jd-fix-agent") {
		t.Fatalf("task keys = %v", task.Keys())
	}

	fixSrc, _ := agents.GetObject("jd-fix-agent")
	fr, _ := RoleByID(RoleFixAgent)
	fix := RenderAgent(fr, fixSrc, prof("pro", map[string]Assignment{RoleFixAgent: fable}))
	if d, _ := fix.GetString("description"); d != "Judgment Day fix agent. (pro profile) [oc-profiles]" {
		t.Fatalf("description = %q", d)
	}
	if m, _ := fix.GetString("model"); m != "anthropic/fable" || fix.Has("variant") {
		t.Fatal("fix agent model wrong")
	}
	if _, name, ok := SplitProfileKey("jd-fix-agent-pro"); !ok || name != "pro" {
		t.Fatal("jd-fix-agent-pro must be a profile key")
	}
	if _, _, ok := SplitProfileKey("jd-fix-agent"); ok {
		t.Fatal("global jd-fix-agent must not be a profile key")
	}
}

func TestFixAgentClearsBackToGlobal(t *testing.T) {
	p := newPaths(t)
	writeDoc(t, p, wrapDoc(gentleAgents()))
	s := newStore(t, p, prof("alpha", map[string]Assignment{RoleOrchestrator: opus, RoleFixAgent: fable}))
	if _, err := s.Apply(); err != nil {
		t.Fatal(err)
	}
	a := agentsOf(t, readTestFile(t, p.OpencodeJSON()))
	o, _ := a.GetObject("odd-orchestrator-alpha")
	if !a.Has("jd-fix-agent-alpha") || !strings.Contains(mustJSON(t, o), `"jd-fix-agent-alpha": "allow"`) {
		t.Fatal("fix agent not generated/delegated")
	}

	prf := s.Profiles.Profiles[0]
	prf.Set(RoleFixAgent, Assignment{})
	s.Profiles.Upsert(prf)
	r, err := s.Apply()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(r.Removed, ",") != "jd-fix-agent-alpha" {
		t.Fatalf("removed = %v", r.Removed)
	}
	a = agentsOf(t, readTestFile(t, p.OpencodeJSON()))
	o, _ = a.GetObject("odd-orchestrator-alpha")
	js := mustJSON(t, o)
	if !strings.Contains(js, `"jd-fix-agent": "allow"`) || strings.Contains(js, "jd-fix-agent-alpha") {
		t.Fatal("orchestrator should point back at the global fix agent")
	}
	if !a.Has("jd-fix-agent") {
		t.Fatal("global fix agent must stay")
	}
}

func TestValidationGateFixAgentTarget(t *testing.T) {
	agents := gentleAgents()
	for _, k := range []string{"odd-explore-x", "odd-worker-x", "odd-verify-x"} {
		agents.Set(k, obj("mode", "subagent"))
	}
	agents.Set("odd-orchestrator-x", obj("permission", obj("task", obj("jd-fix-agent-x", "allow")), "prompt", "use jd-fix-agent-x"))
	p := Profiles{Profiles: []Profile{prof("x", map[string]Assignment{RoleFixAgent: fable})}}
	err := Validate(agents, p)
	if err == nil || !strings.Contains(err.Error(), "jd-fix-agent-x: missing from generated config") ||
		!strings.Contains(err.Error(), `task target "jd-fix-agent-x" does not exist`) {
		t.Fatalf("err = %v", err)
	}
	agents.Set("jd-fix-agent-x", obj("mode", "subagent"))
	if err := Validate(agents, p); err != nil {
		t.Fatalf("valid config refused: %v", err)
	}
}

func TestApplyIgnoresFormattingOnlyDifferences(t *testing.T) {
	p := newPaths(t)
	writeDoc(t, p, wrapDoc(gentleAgents()))
	s := newStore(t, p, prof("alpha", map[string]Assignment{RoleOrchestrator: opus}))
	if _, err := s.Apply(); err != nil {
		t.Fatal(err)
	}
	doc, _ := ojson.ParseObject(readTestFile(t, p.OpencodeJSON()))

	// gentle-ai rewrites the file with Go's default HTML escaping.
	escaped, _ := ojson.MarshalOpts(doc, true)
	escaped = append(escaped, '\n')
	escB := []byte(`\` + "u003cb" + `\` + "u003e")
	if !bytes.Contains(escaped, escB) {
		t.Fatal("fixture should contain escaped HTML")
	}
	writeTestFile(t, p.OpencodeJSON(), escaped)
	if r, err := s.Apply(); err != nil || r.Changed || r.Backup != "" {
		t.Fatalf("escaping-only difference rewrote the file: %+v %v", r, err)
	}
	if !bytes.Equal(readTestFile(t, p.OpencodeJSON()), escaped) {
		t.Fatal("file bytes changed")
	}

	// A real change keeps the file's escaping style.
	s.Profiles.Upsert(prof("beta", map[string]Assignment{RoleOrchestrator: gpt}))
	if _, err := s.Apply(); err != nil {
		t.Fatal(err)
	}
	after := readTestFile(t, p.OpencodeJSON())
	if !bytes.Contains(after, escB) || bytes.Contains(after, []byte("<b>")) {
		t.Fatal("escaping style not preserved")
	}

	// Different indentation only: also not a change.
	var compact bytes.Buffer
	if err := json.Compact(&compact, after); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, p.OpencodeJSON(), compact.Bytes())
	if r, err := s.Apply(); err != nil || r.Changed {
		t.Fatalf("indentation-only difference rewrote the file: %+v %v", r, err)
	}
}
