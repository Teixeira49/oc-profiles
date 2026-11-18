package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Teixeira49/oc-profiles/internal/ojson"
)

const orchestratorPromptFixture = "<!-- gentle-ai:orchestrator -->\n# Gentle AI — ODD Orchestrator Instructions\n\n" +
	"Bind this to the dedicated `gentle-orchestrator` agent only.\n" +
	"Delegate mapping to `gentle-ai-explore`, writing to gentle-ai-worker and checks to \"gentle-ai-verify\".\n" +
	"Read `agent.gentle-orchestrator.model` from `opencode.json`.\n" +
	"Never confuse with gentle-ai-explorer, my-gentle-ai-worker, gentle-orchestrator-legacy or gentle-ai.\n" +
	"Judges: jd-judge-a, jd-judge-b, fixes: jd-fix-agent, review-risk.\n"

func obj(pairs ...any) *ojson.Object {
	o := ojson.NewObject()
	for i := 0; i < len(pairs); i += 2 {
		o.Set(pairs[i].(string), pairs[i+1])
	}
	return o
}

func denyAll() *ojson.Object {
	return obj("edit", "deny", "task", "deny", "write", "deny")
}

// gentleAgents returns the gentle-ai 4.5-shaped agents in file order.
func gentleAgents() *ojson.Object {
	a := ojson.NewObject()
	a.Set("gentle-ai-explore", obj("description", "Read-only exploration and mapping for generic ODD work.", "hidden", true, "mode", "subagent",
		"model", "anthropic/opus", "permission", denyAll(), "prompt", "You are the explorer.", "variant", "xhigh"))
	a.Set("gentle-ai-verify", obj("description", "Read-only technical verification for generic ODD work.", "hidden", true, "mode", "subagent",
		"model", "anthropic/opus", "permission", denyAll(), "prompt", "You are the verifier.", "variant", "high"))
	a.Set("gentle-ai-worker", obj("description", "Scoped implementation writer.", "hidden", true, "mode", "subagent",
		"model", "anthropic/opus", "permission", obj("task", "deny"), "prompt", "You are the worker <b>&</b>.", "variant", "high"))
	a.Set("gentle-orchestrator", obj("model", "anthropic/opus",
		"permission", obj("question", "allow", "task", obj(
			"*", "deny", "gentle-ai-explore", "allow", "gentle-ai-verify", "allow", "gentle-ai-worker", "allow",
			"jd-fix-agent", "allow", "jd-judge-a", "allow", "jd-judge-b", "allow", "review-risk", "allow")),
		"prompt", orchestratorPromptFixture, "variant", "high"))
	a.Set("jd-fix-agent", obj("description", "Judgment Day fix agent.", "hidden", true, "mode", "subagent", "model", "anthropic/sonnet",
		"permission", obj("task", "deny"), "prompt", "Fix.", "variant", "high"))
	a.Set("jd-judge-a", obj("description", "Judgment Day blind adversarial reviewer A.", "hidden", true, "mode", "subagent",
		"model", "anthropic/fable", "permission", denyAll(), "prompt", "You are judge A.", "variant", "high"))
	a.Set("jd-judge-b", obj("description", "Judgment Day blind adversarial reviewer B.", "hidden", true, "mode", "subagent",
		"model", "anthropic/opus", "permission", denyAll(), "prompt", "You are judge B.", "variant", "high"))
	a.Set("review-risk", obj("description", "R1 Risk reviewer.", "hidden", true, "mode", "subagent", "permission", denyAll(), "prompt", "Review."))
	return a
}

func wrapDoc(agents *ojson.Object) *ojson.Object {
	doc := ojson.NewObject()
	doc.Set("$schema", "https://opencode.ai/config.json")
	doc.Set("agent", agents)
	doc.Set("default_agent", "gentle-orchestrator")
	doc.Set("mcp", obj("engram", obj("command", []any{"engram", "mcp"}, "enabled", true, "timeout", json.Number("30000"))))
	doc.Set("plugin", []any{"a", "b"})
	doc.Set("share", "disabled")
	return doc
}

func newPaths(t *testing.T) Paths {
	t.Helper()
	root := t.TempDir()
	Now = func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) }
	t.Cleanup(func() { Now = time.Now })
	return Paths{
		OpencodeDir:  filepath.Join(root, "opencode"),
		StoreDir:     filepath.Join(root, "oc-profiles"),
		VariantsFile: filepath.Join(root, "variants.json"),
	}
}

func writeDoc(t *testing.T, p Paths, doc *ojson.Object) []byte {
	t.Helper()
	data, err := ojson.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	writeTestFile(t, p.OpencodeJSON(), data)
	return data
}

// newStore writes a v2 store with the given profiles and an up-to-date
// snapshot of the gentle-ai sources.
func newStore(t *testing.T, p Paths, profiles ...Profile) *Store {
	t.Helper()
	_, agents, _, err := ReadOpencode(p)
	if err != nil {
		t.Fatal(err)
	}
	s := &Store{Paths: p, Profiles: Profiles{Version: StoreVersion, Profiles: profiles}, Sources: snapshotSources(agents)}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadStore(p)
	if err != nil {
		t.Fatal(err)
	}
	return loaded
}

func prof(name string, models map[string]Assignment) Profile {
	return Profile{Name: name, Models: models}
}

func writeTestFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func readTestFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func agentsOf(t *testing.T, data []byte) *ojson.Object {
	t.Helper()
	doc, err := ojson.ParseObject(data)
	if err != nil {
		t.Fatal(err)
	}
	a, ok := doc.GetObject("agent")
	if !ok {
		t.Fatal("no agent map")
	}
	return a
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := ojson.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
