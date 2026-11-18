package core

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Assignment is a model ("provider/model-id") plus an optional reasoning
// effort variant. An empty Model means "use the OpenCode default".
type Assignment struct {
	Model   string `json:"model,omitempty"`
	Variant string `json:"variant,omitempty"`
}

// IsSet reports whether a model is assigned.
func (a Assignment) IsSet() bool { return a.Model != "" }

// Provider returns the provider part of Model.
func (a Assignment) Provider() string {
	p, _, _ := strings.Cut(a.Model, "/")
	return p
}

// ModelID returns the model part of Model (may itself contain slashes).
func (a Assignment) ModelID() string {
	_, m, _ := strings.Cut(a.Model, "/")
	return m
}

// Label renders "provider/model [variant]" or fallback when unset.
func (a Assignment) Label(fallback string) string {
	if !a.IsSet() {
		return fallback
	}
	if a.Variant != "" {
		return a.Model + " [" + a.Variant + "]"
	}
	return a.Model
}

// Profile is one named ODD profile. Models is keyed by role ID; unset roles
// fall back to the OpenCode default model, and unset optional roles (judges,
// fix agent) are not generated: the global gentle-ai agents are used instead.
type Profile struct {
	Name   string                `json:"name"`
	Models map[string]Assignment `json:"models"`
}

// Get returns the assignment for a role ID.
func (p Profile) Get(role string) Assignment { return p.Models[role] }

// Set stores an assignment; an unset assignment removes the entry.
func (p *Profile) Set(role string, a Assignment) {
	if p.Models == nil {
		p.Models = map[string]Assignment{}
	}
	if !a.IsSet() {
		delete(p.Models, role)
		return
	}
	p.Models[role] = a
}

// ActiveRoles returns the roles that produce agents for this profile.
func (p Profile) ActiveRoles() []Role {
	var out []Role
	for _, r := range roles {
		if r.Optional && !p.Get(r.ID).IsSet() {
			continue
		}
		out = append(out, r)
	}
	return out
}

// AgentKeys returns every opencode.json agent key generated for this profile.
func (p Profile) AgentKeys() []string {
	var keys []string
	for _, r := range p.ActiveRoles() {
		keys = append(keys, r.AgentKey(p.Name))
	}
	return keys
}

// Clone deep-copies the profile.
func (p Profile) Clone() Profile {
	c := Profile{Name: p.Name, Models: make(map[string]Assignment, len(p.Models))}
	for k, v := range p.Models {
		c.Models[k] = v
	}
	return c
}

// Profiles is the content of profiles.json (schema version 2).
type Profiles struct {
	Version int `json:"version"`
	// SDDCleanup is set by `migrate`: the next successful apply removes the
	// retired gentle-ai 3.7 SDD agents and asset files, then clears it.
	SDDCleanup bool      `json:"sddCleanup,omitempty"`
	Profiles   []Profile `json:"profiles"`
}

// Find returns the index of the named profile or -1.
func (ps *Profiles) Find(name string) int {
	for i, p := range ps.Profiles {
		if p.Name == name {
			return i
		}
	}
	return -1
}

// Upsert replaces or adds a profile and keeps the list sorted by name.
func (ps *Profiles) Upsert(p Profile) {
	if i := ps.Find(p.Name); i >= 0 {
		ps.Profiles[i] = p
	} else {
		ps.Profiles = append(ps.Profiles, p)
	}
	ps.Sort()
}

// Remove deletes the named profile. It reports whether it existed.
func (ps *Profiles) Remove(name string) bool {
	i := ps.Find(name)
	if i < 0 {
		return false
	}
	ps.Profiles = append(ps.Profiles[:i], ps.Profiles[i+1:]...)
	return true
}

// Sort orders profiles by name.
func (ps *Profiles) Sort() {
	sort.Slice(ps.Profiles, func(i, j int) bool { return ps.Profiles[i].Name < ps.Profiles[j].Name })
}

// Names returns the profile names in order.
func (ps *Profiles) Names() []string {
	names := make([]string, len(ps.Profiles))
	for i, p := range ps.Profiles {
		names[i] = p.Name
	}
	return names
}

// ErrRenameNoop is returned by Rename when the new name equals the old one.
var ErrRenameNoop = errors.New("new name is the same as the current name")

// Rename changes a profile's name, keeping every model assignment. The new
// name must be a valid slug and must not belong to another profile. Applying
// the store afterwards removes the old agents and generates the new ones.
func (ps *Profiles) Rename(oldName, newName string) error {
	i := ps.Find(oldName)
	if i < 0 {
		return fmt.Errorf("profile %q does not exist", oldName)
	}
	if newName == oldName {
		return ErrRenameNoop
	}
	if err := ValidateProfileName(newName); err != nil {
		return err
	}
	if ps.Find(newName) >= 0 {
		return fmt.Errorf("profile %q already exists", newName)
	}
	ps.Profiles[i].Name = newName
	ps.Sort()
	return nil
}
