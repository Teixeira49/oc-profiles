package core

import (
	"fmt"
	"regexp"
	"strings"
)

// Role identifies one agent slot of an ODD profile.
type Role struct {
	ID       string // key in profiles.json ("orchestrator", "explore", ...)
	Label    string // TUI label
	Prefix   string // generated agent key is Prefix + "-" + profile
	Source   string // live gentle-ai agent the profile agent is cloned from
	Optional bool   // generated only when a model is assigned (judges)
}

// Role IDs.
const (
	RoleOrchestrator = "orchestrator"
	RoleExplore      = "explore"
	RoleWorker       = "worker"
	RoleVerify       = "verify"
	RoleJudgeA       = "jd-judge-a"
	RoleJudgeB       = "jd-judge-b"
	RoleFixAgent     = "jd-fix-agent"
)

// Marker is appended to every generated description. gentle-ai's sync only
// retires agents whose description matches a released gentle-ai value, so the
// marker keeps profile agents user-owned.
const Marker = "[oc-profiles]"

var roles = []Role{
	{ID: RoleOrchestrator, Label: "Orchestrator", Prefix: "odd-orchestrator", Source: "gentle-orchestrator"},
	{ID: RoleExplore, Label: "Explore", Prefix: "odd-explore", Source: "gentle-ai-explore"},
	{ID: RoleWorker, Label: "Worker", Prefix: "odd-worker", Source: "gentle-ai-worker"},
	{ID: RoleVerify, Label: "Verify", Prefix: "odd-verify", Source: "gentle-ai-verify"},
	{ID: RoleJudgeA, Label: "Judge A", Prefix: "jd-judge-a", Source: "jd-judge-a", Optional: true},
	{ID: RoleJudgeB, Label: "Judge B", Prefix: "jd-judge-b", Source: "jd-judge-b", Optional: true},
	{ID: RoleFixAgent, Label: "Fix Agent", Prefix: "jd-fix-agent", Source: "jd-fix-agent", Optional: true},
}

// Roles returns every ODD profile role in canonical order.
func Roles() []Role { return append([]Role(nil), roles...) }

// SubagentRoleIDs are the roles set by "Set all subagents".
func SubagentRoleIDs() []string { return []string{RoleExplore, RoleWorker, RoleVerify} }

// RoleByID looks up a role.
func RoleByID(id string) (Role, bool) {
	for _, r := range roles {
		if r.ID == id {
			return r, true
		}
	}
	return Role{}, false
}

// SourceNames lists the live gentle-ai agents used as templates.
func SourceNames() []string {
	out := make([]string, len(roles))
	for i, r := range roles {
		out[i] = r.Source
	}
	return out
}

// AgentKey returns the opencode.json key of role r in profile name.
func (r Role) AgentKey(name string) string { return r.Prefix + "-" + name }

var profileNameRegex = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

var reservedNames = map[string]bool{
	"default":      true,
	"orchestrator": true,
	"jd-judge-a":   true,
	"jd-judge-b":   true,
	"jd-fix-agent": true,
}

// ValidateProfileName enforces the gentle-ai 3.7 slug rules.
func ValidateProfileName(name string) error {
	if name == "" {
		return fmt.Errorf("profile name must not be empty")
	}
	if reservedNames[name] {
		return fmt.Errorf("profile name %q is reserved", name)
	}
	if !profileNameRegex.MatchString(name) {
		return fmt.Errorf("profile name %q must be lowercase alphanumeric with hyphens (no leading/trailing hyphen)", name)
	}
	return nil
}

// SplitProfileKey parses "<prefix>-<profile>" for ODD profile agent keys.
func SplitProfileKey(key string) (Role, string, bool) {
	var best Role
	bestName := ""
	for _, r := range roles {
		name, ok := strings.CutPrefix(key, r.Prefix+"-")
		if !ok || !profileNameRegex.MatchString(name) {
			continue
		}
		if len(r.Prefix) > len(best.Prefix) {
			best, bestName = r, name
		}
	}
	return best, bestName, bestName != ""
}

// Legacy SDD names (gentle-ai 3.7 profiles), removed after migration.
var legacySDDPhases = []string{
	"sdd-init", "sdd-explore", "sdd-research", "sdd-propose", "sdd-spec", "sdd-design",
	"sdd-tasks", "sdd-apply", "sdd-verify", "sdd-archive", "sdd-onboard",
}

// IsLegacySDDKey reports whether key is an SDD phase base agent (sdd-apply)
// or an SDD profile agent (sdd-orchestrator-<name>, sdd-apply-<name>).
func IsLegacySDDKey(key string) bool {
	for _, base := range append([]string{"sdd-orchestrator"}, legacySDDPhases...) {
		if key == base {
			return base != "sdd-orchestrator"
		}
		if name, ok := strings.CutPrefix(key, base+"-"); ok && profileNameRegex.MatchString(name) {
			return true
		}
	}
	return false
}

func isNameChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_'
}

// ReplaceBounded replaces whole-token occurrences of from with to.
//
// A token is bounded when the neighbouring characters are not slug
// characters, so "gentle-ai-explore" never matches inside
// "gentle-ai-explorer" or "my-gentle-ai-explore". Occurrences in a path or
// file-reference context are left alone: preceded by '/', or followed by
// '/', ':' or a file extension ('.' + letter/digit), e.g.
// "prompts/gentle-ai-worker.md", "skills/gentle-ai-verify/SKILL.md" or
// "{file:./gentle-ai-worker.md}". A sentence-ending period still allows the
// rewrite, and so does the OpenCode config path "agent.<name>.model".
func ReplaceBounded(s, from, to string) string {
	if from == "" || !strings.Contains(s, from) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		idx := strings.Index(s[i:], from)
		if idx < 0 {
			b.WriteString(s[i:])
			break
		}
		start := i + idx
		end := start + len(from)
		b.WriteString(s[i:start])
		if replaceable(s, start, end) {
			b.WriteString(to)
		} else {
			b.WriteString(from)
		}
		i = end
	}
	return b.String()
}

func replaceable(s string, start, end int) bool {
	if start > 0 && isNameChar(s[start-1]) {
		return false
	}
	if end < len(s) && isNameChar(s[end]) {
		return false
	}
	if start > 0 && s[start-1] == '/' {
		return false
	}
	if end < len(s) {
		switch s[end] {
		case '/', ':':
			return false
		case '.':
			configPath := strings.HasSuffix(s[:start], "agent.")
			if !configPath && end+1 < len(s) && isAlnum(s[end+1]) {
				return false
			}
		}
	}
	return true
}

func isAlnum(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// ContainsBounded reports whether token occurs as a whole token in s.
func ContainsBounded(s, token string) bool {
	return ReplaceBounded(s, token, "\x00") != s
}
