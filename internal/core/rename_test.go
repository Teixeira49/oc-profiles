package core

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/Teixeira49/oc-profiles/internal/ojson"
)

// profileAgentJSON collects the rendered agents of one profile, with the
// profile name normalized so different names can be compared.
func profileAgentJSON(t *testing.T, agents *ojson.Object, name string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, k := range agents.Keys() {
		_, n, ok := SplitProfileKey(k)
		if !ok || n != name {
			continue
		}
		v, _ := agents.Get(k)
		js := mustJSON(t, v)
		role := strings.TrimSuffix(k, "-"+name)
		out[role] = strings.ReplaceAll(js, name, "<P>")
	}
	return out
}

func TestRenameRoundTripKeepsAssignmentsAndBytes(t *testing.T) {
	p := newPaths(t)
	writeDoc(t, p, wrapDoc(gentleAgents()))
	full := map[string]Assignment{RoleOrchestrator: opus, RoleExplore: gpt, RoleWorker: fable, RoleVerify: gpt,
		RoleJudgeA: fable, RoleJudgeB: opus, RoleFixAgent: gpt}
	s := newStore(t, p, prof("chappie", full), prof("chappie-ssj", map[string]Assignment{RoleOrchestrator: gpt, RoleJudgeA: opus}))
	if _, err := s.Apply(); err != nil {
		t.Fatal(err)
	}
	orig := readTestFile(t, p.OpencodeJSON())
	before := agentsOf(t, orig)
	ssjBefore, _ := before.GetObject("odd-orchestrator-chappie-ssj")

	if err := s.Profiles.Rename("chappie", "chappie-test"); err != nil {
		t.Fatal(err)
	}
	r, err := s.Apply()
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Removed) != 7 || len(r.Added) != 7 || len(r.Updated) != 0 {
		t.Fatalf("rename apply: added %v removed %v updated %v", r.Added, r.Removed, r.Updated)
	}
	for _, k := range r.Removed {
		if !strings.HasSuffix(k, "-chappie") {
			t.Fatalf("removed unexpected %s", k)
		}
	}
	after := agentsOf(t, readTestFile(t, p.OpencodeJSON()))
	if got, want := profileAgentJSON(t, after, "chappie-test"), profileAgentJSON(t, before, "chappie"); mustJSON(t, toObj(got)) != mustJSON(t, toObj(want)) {
		t.Fatal("renamed agents differ from the originals beyond the name")
	}
	ssjAfter, _ := after.GetObject("odd-orchestrator-chappie-ssj")
	if mustJSON(t, ssjBefore) != mustJSON(t, ssjAfter) {
		t.Fatal("chappie-ssj changed")
	}
	if mustJSON(t, s.Profiles.Profiles[s.Profiles.Find("chappie-test")].Models) != mustJSON(t, full) {
		t.Fatal("assignments lost")
	}

	// Idempotent after rename.
	snap := readTestFile(t, p.OpencodeJSON())
	if r, err := s.Apply(); err != nil || !r.InSync() || !bytes.Equal(snap, readTestFile(t, p.OpencodeJSON())) {
		t.Fatalf("apply after rename not idempotent: %+v %v", r, err)
	}

	// And back: byte-identical to the original.
	if err := s.Profiles.Rename("chappie-test", "chappie"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(orig, readTestFile(t, p.OpencodeJSON())) {
		t.Fatal("round-trip rename is not byte-identical")
	}
}

func toObj(m map[string]string) *ojson.Object {
	o := ojson.NewObject()
	for _, k := range []string{"odd-orchestrator", "odd-explore", "odd-worker", "odd-verify", "jd-judge-a", "jd-judge-b", "jd-fix-agent"} {
		if v, ok := m[k]; ok {
			o.Set(k, v)
		}
	}
	return o
}

func TestRenamePrefixAndSuffixCollisions(t *testing.T) {
	p := newPaths(t)
	writeDoc(t, p, wrapDoc(gentleAgents()))
	s := newStore(t, p,
		prof("chappie", map[string]Assignment{RoleOrchestrator: opus, RoleJudgeA: fable, RoleFixAgent: gpt}),
		prof("chappie-ssj", map[string]Assignment{RoleOrchestrator: gpt, RoleJudgeA: opus, RoleFixAgent: fable}),
		prof("ssj", map[string]Assignment{RoleOrchestrator: fable}))
	if _, err := s.Apply(); err != nil {
		t.Fatal(err)
	}
	others := func(a *ojson.Object) string {
		return mustJSON(t, toObj(profileAgentJSON(t, a, "chappie-ssj"))) + mustJSON(t, toObj(profileAgentJSON(t, a, "ssj")))
	}
	base := others(agentsOf(t, readTestFile(t, p.OpencodeJSON())))

	// New name is a prefix of another profile, then a suffix-shaped name.
	for _, step := range [][2]string{{"chappie", "chap"}, {"chap", "x-ssj"}, {"x-ssj", "chappie-ss"}, {"chappie-ss", "chappie"}} {
		if err := s.Profiles.Rename(step[0], step[1]); err != nil {
			t.Fatalf("%v: %v", step, err)
		}
		r, err := s.Apply()
		if err != nil {
			t.Fatalf("%v: %v", step, err)
		}
		for _, k := range r.Removed {
			if !strings.HasSuffix(k, "-"+step[0]) {
				t.Fatalf("%v removed %s", step, k)
			}
		}
		a := agentsOf(t, readTestFile(t, p.OpencodeJSON()))
		if others(a) != base {
			t.Fatalf("%v changed chappie-ssj or ssj", step)
		}
		o, _ := a.GetObject("odd-orchestrator-" + step[1])
		js := mustJSON(t, o)
		for _, want := range []string{`"jd-judge-a-` + step[1] + `": "allow"`, `"jd-fix-agent-` + step[1] + `": "allow"`, `"odd-worker-` + step[1] + `": "allow"`} {
			if !strings.Contains(js, want) {
				t.Fatalf("%v: orchestrator missing %s", step, want)
			}
		}
		if strings.Contains(js, "chappie-ssj") || strings.Contains(js, `-ssj"`) && step[1] != "x-ssj" {
			t.Fatalf("%v: orchestrator references another profile", step)
		}
	}
}

func TestRenameRefusals(t *testing.T) {
	ps := Profiles{Profiles: []Profile{prof("alpha", nil), prof("beta", nil)}}
	if err := ps.Rename("alpha", "alpha"); !errors.Is(err, ErrRenameNoop) {
		t.Fatalf("same name: %v", err)
	}
	if err := ps.Rename("alpha", "beta"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("duplicate: %v", err)
	}
	for _, bad := range []string{"", "Bad", "a_b", "-x", "default", "jd-fix-agent"} {
		if ps.Rename("alpha", bad) == nil {
			t.Fatalf("%q accepted", bad)
		}
	}
	if err := ps.Rename("ghost", "gamma"); err == nil {
		t.Fatal("missing profile accepted")
	}
	if strings.Join(ps.Names(), ",") != "alpha,beta" {
		t.Fatalf("failed renames mutated the store: %v", ps.Names())
	}
}
