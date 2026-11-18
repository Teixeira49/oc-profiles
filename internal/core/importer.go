package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/teixeira49/oc-profiles/internal/ojson"
)

// ImportResult summarizes an import.
type ImportResult struct {
	Profiles []string
	// FromSDD is true when no ODD profiles existed and gentle-ai 3.7 SDD
	// profile agents were converted instead.
	FromSDD        bool
	MissingSources []string
	Warnings       []string
	// MovedAside lists previous store files renamed by --force.
	MovedAside []string
}

// ImportOptions controls Import.
type ImportOptions struct {
	Force      bool // replace an existing store (the old files are moved aside)
	AllowEmpty bool // with Force, allow replacing a non-empty store with an empty one
}

// ErrImportWouldEmpty is returned when --force would replace a store that has
// profiles with one that has none.
var ErrImportWouldEmpty = errors.New("import found no profiles but the existing store has some; pass --allow-empty to replace it anyway")

// ReadOpencode parses opencode.json and returns the document, its agent map
// and the raw bytes.
func ReadOpencode(p Paths) (*ojson.Object, *ojson.Object, []byte, error) {
	data, err := os.ReadFile(p.OpencodeJSON())
	if err != nil {
		return nil, nil, nil, err
	}
	doc, err := ojson.ParseObject(data)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("parse %s: %w", p.OpencodeJSON(), err)
	}
	raw, present := doc.Get("agent")
	agents, ok := raw.(*ojson.Object)
	switch {
	case !present:
		agents = ojson.NewObject()
	case !ok:
		return nil, nil, nil, fmt.Errorf("%s: \"agent\" must be an object, found %s", p.OpencodeJSON(), jsonKind(raw))
	}
	return doc, agents, data, nil
}

func jsonKind(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case string:
		return "a string"
	case []any:
		return "an array"
	case bool:
		return "a boolean"
	default:
		return "a number"
	}
}

func assignmentOf(agents *ojson.Object, key string) Assignment {
	agent, ok := agents.GetObject(key)
	if !ok {
		return Assignment{}
	}
	m, _ := agent.GetString("model")
	v, _ := agent.GetString("variant")
	if m == "" {
		return Assignment{}
	}
	return Assignment{Model: m, Variant: v}
}

// DetectODDProfiles extracts ODD profiles: one per odd-orchestrator-<name>.
// Reserved or invalid names are skipped with a warning.
func DetectODDProfiles(agents *ojson.Object) (Profiles, []string) {
	ps := Profiles{Version: StoreVersion}
	var warnings []string
	for _, key := range agents.Keys() {
		r, name, ok := SplitProfileKey(key)
		if !ok || r.ID != RoleOrchestrator {
			continue
		}
		if err := ValidateProfileName(name); err != nil {
			warnings = append(warnings, fmt.Sprintf("skipped %s: %v", key, err))
			continue
		}
		p := Profile{Name: name, Models: map[string]Assignment{}}
		for _, role := range roles {
			p.Set(role.ID, assignmentOf(agents, role.AgentKey(name)))
		}
		ps.Profiles = append(ps.Profiles, p)
	}
	ps.Sort()
	return ps, warnings
}

// SDDToODD maps a gentle-ai 3.7 SDD profile onto the ODD roles:
// orchestrator <- sdd-orchestrator, explore <- sdd-explore,
// worker <- sdd-apply, verify <- sdd-verify, judges <- judges.
func SDDToODD(name string, orchestrator Assignment, phases map[string]Assignment) Profile {
	p := Profile{Name: name, Models: map[string]Assignment{}}
	p.Set(RoleOrchestrator, orchestrator)
	p.Set(RoleExplore, phases["sdd-explore"])
	p.Set(RoleWorker, phases["sdd-apply"])
	p.Set(RoleVerify, phases["sdd-verify"])
	p.Set(RoleJudgeA, phases["jd-judge-a"])
	p.Set(RoleJudgeB, phases["jd-judge-b"])
	return p
}

// detectSDDProfiles converts sdd-orchestrator-<name> profiles in opencode.json.
func detectSDDProfiles(agents *ojson.Object) Profiles {
	ps := Profiles{Version: StoreVersion}
	for _, key := range agents.Keys() {
		name, ok := cutProfile(key, "sdd-orchestrator")
		if !ok || ValidateProfileName(name) != nil {
			continue
		}
		phases := map[string]Assignment{}
		for _, ph := range []string{"sdd-explore", "sdd-apply", "sdd-verify", "jd-judge-a", "jd-judge-b"} {
			phases[ph] = assignmentOf(agents, ph+"-"+name)
		}
		ps.Profiles = append(ps.Profiles, SDDToODD(name, assignmentOf(agents, key), phases))
	}
	ps.Sort()
	return ps
}

func cutProfile(key, prefix string) (string, bool) {
	if len(key) <= len(prefix)+1 || key[:len(prefix)+1] != prefix+"-" {
		return "", false
	}
	name := key[len(prefix)+1:]
	return name, profileNameRegex.MatchString(name)
}

// Import builds a fresh v2 store from opencode.json. It reads existing ODD
// profiles; when there are none it converts gentle-ai 3.7 SDD profiles and
// schedules the SDD cleanup. An empty store is valid.
func Import(p Paths, opts ImportOptions) (*Store, ImportResult, error) {
	var res ImportResult
	exists := StoreExists(p)
	if exists && !opts.Force {
		return nil, res, ErrStoreExists
	}
	_, agents, _, err := ReadOpencode(p)
	if err != nil {
		return nil, res, err
	}
	profiles, warnings := DetectODDProfiles(agents)
	res.Warnings = warnings
	if len(profiles.Profiles) == 0 {
		if sdd := detectSDDProfiles(agents); len(sdd.Profiles) > 0 {
			profiles = sdd
			profiles.SDDCleanup = true
			res.FromSDD = true
		}
	}
	sources := snapshotSources(agents)
	for _, name := range SourceNames() {
		if !sources.Has(name) {
			res.MissingSources = append(res.MissingSources, name)
		}
	}
	res.Profiles = profiles.Names()
	if exists {
		if len(profiles.Profiles) == 0 && !opts.AllowEmpty && existingProfileCount(p) > 0 {
			return nil, res, ErrImportWouldEmpty
		}
		moved, err := moveStoreAside(p, "replaced")
		res.MovedAside = moved
		if err != nil {
			return nil, res, err
		}
	}
	s := &Store{Paths: p, Profiles: profiles, Sources: sources}
	if err := s.Save(); err != nil {
		return nil, res, err
	}
	return s, res, nil
}

// existingProfileCount counts profiles in the current profiles.json, reading
// it leniently (any schema); an unreadable file counts as 0.
func existingProfileCount(p Paths) int {
	data, err := os.ReadFile(p.ProfilesFile())
	if err != nil {
		return 0
	}
	var head struct {
		Profiles []json.RawMessage `json:"profiles"`
	}
	if json.Unmarshal(data, &head) != nil {
		return 0
	}
	return len(head.Profiles)
}
