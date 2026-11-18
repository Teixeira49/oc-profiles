package core

import (
	"fmt"
	"strings"

	"github.com/Teixeira49/oc-profiles/internal/ojson"
)

// builtinAgents are provided by OpenCode itself and need no config entry.
var builtinAgents = map[string]bool{"build": true, "plan": true, "general": true, "explore": true}

// renames returns the gentle-ai name -> profile name rewrites for p.
// Optional roles (judges, fix agent) are only redirected when assigned.
func renames(p Profile) [][2]string {
	var out [][2]string
	for _, r := range roles {
		if r.Optional && !p.Get(r.ID).IsSet() {
			continue
		}
		out = append(out, [2]string{r.Source, r.AgentKey(p.Name)})
	}
	return out
}

func rewrite(s string, pairs [][2]string) string {
	for _, pr := range pairs {
		s = ReplaceBounded(s, pr[0], pr[1])
	}
	return s
}

// RenderAgent clones the live gentle-ai source agent into the profile agent
// for role r.
func RenderAgent(r Role, source *ojson.Object, p Profile) *ojson.Object {
	agent := ojson.CloneObject(source)
	agent.Delete("model")
	agent.Delete("variant")
	agent.Delete("__managed_by")
	pairs := renames(p)

	if r.ID == RoleOrchestrator {
		if prompt, ok := agent.GetString("prompt"); ok {
			agent.Set("prompt", rewrite(prompt, pairs))
		}
		agent.SetSorted("description", fmt.Sprintf("ODD Orchestrator (%s profile) %s", p.Name, Marker))
		agent.SetSorted("mode", "primary")
		agent.Delete("hidden")
	} else {
		desc, _ := agent.GetString("description")
		desc = strings.TrimSpace(desc)
		if desc == "" {
			desc = r.Label
		}
		agent.SetSorted("description", fmt.Sprintf("%s (%s profile) %s", desc, p.Name, Marker))
	}

	// Redirect delegation targets in the task allow-list.
	if perm, ok := agent.GetObject("permission"); ok {
		if task, ok := perm.GetObject("task"); ok {
			renamed := ojson.NewObject()
			for _, k := range task.Keys() {
				v, _ := task.Get(k)
				renamed.Set(rewrite(k, pairs), v)
			}
			perm.Set("task", renamed)
		}
	}

	if a := p.Get(r.ID); a.IsSet() {
		agent.SetSorted("model", a.Model)
		if a.Variant != "" {
			agent.SetSorted("variant", a.Variant)
		}
	}
	return agent
}

// Validate is the gate run before writing opencode.json: every allowed task
// target of a profile agent must exist, and profile orchestrator prompts must
// not mention the gentle-ai agents they replace.
func Validate(agents *ojson.Object, profiles Profiles) error {
	var problems []string
	for _, p := range profiles.Profiles {
		for _, r := range p.ActiveRoles() {
			key := r.AgentKey(p.Name)
			agent, ok := agents.GetObject(key)
			if !ok {
				problems = append(problems, key+": missing from generated config")
				continue
			}
			if perm, ok := agent.GetObject("permission"); ok {
				if task, ok := perm.GetObject("task"); ok {
					for _, target := range task.Keys() {
						v, _ := task.Get(target)
						if v != "allow" || strings.ContainsAny(target, "*?") {
							continue
						}
						if !agents.Has(target) && !builtinAgents[target] {
							problems = append(problems, fmt.Sprintf("%s: task target %q does not exist", key, target))
						}
					}
				}
			}
			if r.ID != RoleOrchestrator {
				continue
			}
			prompt, _ := agent.GetString("prompt")
			for _, src := range []string{"gentle-ai-explore", "gentle-ai-worker", "gentle-ai-verify", "gentle-orchestrator"} {
				if ContainsBounded(prompt, src) {
					problems = append(problems, fmt.Sprintf("%s: prompt still references %q", key, src))
				}
			}
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("validation failed, nothing written:\n  - %s", strings.Join(problems, "\n  - "))
	}
	return nil
}
