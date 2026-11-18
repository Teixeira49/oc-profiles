package core

import (
	"os"
	"strings"

	"github.com/Teixeira49/oc-profiles/internal/ojson"
)

// gentle-ai's own ownership marker for SDD agents it generated.
const gentleSDDMarker = "gentle-ai/sdd"

// v1 (gentle-ai 3.7 SDD profile) template markers.
const (
	v1Placeholder      = "{{PROFILE}}"
	v1TablePlaceholder = "{{MODEL_ASSIGNMENTS}}"
	v1TableStart       = "<!-- gentle-ai:sdd-model-assignments -->"
	v1TableEnd         = "<!-- /gentle-ai:sdd-model-assignments -->"
	v1JDOverridesHead  = "\n\n### Profile Judgment Day Delegation Overrides"
)

// v1Roles are the agent roles a v1 profile generated.
func v1Roles() []string {
	roles := append([]string{"sdd-orchestrator"}, legacySDDPhases...)
	return append(roles, "jd-judge-a", "jd-judge-b")
}

// legacyTemplates is the v1 snapshot kept by `migrate` (templates-v1.json):
// the per-role profile templates and the unsuffixed base agents.
type legacyTemplates struct {
	profile *ojson.Object
	base    *ojson.Object
}

// loadLegacyTemplates reads templates-v1.json; a missing or unreadable file
// yields empty templates, which own nothing.
func loadLegacyTemplates(p Paths) legacyTemplates {
	lt := legacyTemplates{profile: ojson.NewObject(), base: ojson.NewObject()}
	data, err := os.ReadFile(p.legacyTemplatesFile())
	if err != nil {
		return lt
	}
	doc, err := ojson.ParseObject(data)
	if err != nil {
		return lt
	}
	if o, ok := doc.GetObject("profileAgents"); ok {
		lt.profile = o
	}
	if o, ok := doc.GetObject("baseAgents"); ok {
		lt.base = o
	}
	return lt
}

func managedByGentle(agent *ojson.Object) bool {
	m, _ := agent.GetString("__managed_by")
	return m == gentleSDDMarker
}

// hasMarker reports whether agent carries the oc-profiles ownership marker.
func hasMarker(agent *ojson.Object) bool {
	d, _ := agent.GetString("description")
	return strings.Contains(d, Marker)
}

// owns reports whether a legacy agent is provably gentle-ai/oc-profiles v1
// output: it carries gentle-ai's SDD marker, or it equals the v1 snapshot
// (a base agent verbatim, or a profile agent with its name substituted).
func (lt legacyTemplates) owns(key string, agent *ojson.Object) bool {
	if managedByGentle(agent) {
		return true
	}
	if snap, ok := lt.base.Get(key); ok {
		return sameJSON(snap, agent)
	}
	role, name, ok := splitV1Key(key)
	if !ok {
		return false
	}
	tmpl, ok := lt.profile.GetObject(role)
	if !ok {
		return false
	}
	return sameJSON(tmpl, v1Normalize(role, agent, name))
}

func splitV1Key(key string) (role, name string, ok bool) {
	for _, r := range v1Roles() {
		n, found := strings.CutPrefix(key, r+"-")
		if found && profileNameRegex.MatchString(n) && len(r) > len(role) {
			role, name = r, n
		}
	}
	return role, name, role != ""
}

// v1Normalize reproduces the v1 import transform: drop model/variant, swap
// the profile name for the placeholder (bounded, v1 rules), template the
// orchestrator's model table and drop its generated JD overrides section.
func v1Normalize(role string, agent *ojson.Object, name string) *ojson.Object {
	t := ojson.CloneObject(agent)
	t.Delete("model")
	t.Delete("variant")
	t = mapStrings(t, func(s string) string {
		for _, r := range v1Roles() {
			s = v1ReplaceBounded(s, r+"-"+name, r+"-"+v1Placeholder)
		}
		return strings.ReplaceAll(s, "("+name+" profile)", "("+v1Placeholder+" profile)")
	}).(*ojson.Object)
	if role != "sdd-orchestrator" {
		return t
	}
	prompt, ok := t.GetString("prompt")
	if !ok {
		return t
	}
	if i := strings.Index(prompt, v1TableStart); i >= 0 {
		rest := prompt[i+len(v1TableStart):]
		if j := strings.Index(rest, v1TableEnd); j >= 0 {
			prompt = prompt[:i+len(v1TableStart)] + v1TablePlaceholder + rest[j:]
		}
	}
	if k := strings.Index(prompt, v1JDOverridesHead); k >= 0 {
		prompt = prompt[:k] + "\n"
	}
	t.Set("prompt", prompt)
	return t
}

// v1ReplaceBounded is the v1 token rule: the token must not be adjacent to
// [a-z0-9-] on either side.
func v1ReplaceBounded(s, from, to string) string {
	isName := func(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' }
	if !strings.Contains(s, from) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		idx := strings.Index(s[i:], from)
		if idx < 0 {
			b.WriteString(s[i:])
			break
		}
		start, end := i+idx, i+idx+len(from)
		bounded := (start == 0 || !isName(s[start-1])) && (end == len(s) || !isName(s[end]))
		b.WriteString(s[i:start])
		if bounded {
			b.WriteString(to)
		} else {
			b.WriteString(from)
		}
		i = end
	}
	return b.String()
}

// mapStrings applies f to every string value and object key in v.
func mapStrings(v any, f func(string) string) any {
	switch t := v.(type) {
	case *ojson.Object:
		out := ojson.NewObject()
		for _, k := range t.Keys() {
			val, _ := t.Get(k)
			out.Set(f(k), mapStrings(val, f))
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = mapStrings(e, f)
		}
		return out
	case string:
		return f(t)
	default:
		return v
	}
}
