package tui

import (
	"fmt"
	"strings"

	"github.com/teixeira49/oc-profiles/internal/core"
)

// View renders the current screen inside the gentle-ai double-border frame.
func (m Model) View() string {
	var body string
	switch m.screen {
	case screenImport:
		body = m.viewImport()
	case screenMigrate:
		body = m.viewMigrate()
	case screenList:
		body = m.viewList()
	case screenName:
		body = m.viewName()
	case screenPicker:
		body = m.viewPicker()
	case screenConfirm:
		body = m.viewConfirm()
	case screenDelete:
		body = m.viewDelete()
	case screenRename:
		body = m.viewRename()
	case screenRenameConfirm:
		body = m.viewRenameConfirm()
	case screenBackups:
		body = m.viewBackups()
	case screenRestoreConfirm:
		body = m.viewRestoreConfirm()
	}
	return frameStyle.Render(body)
}

func (m Model) messages() string {
	var b strings.Builder
	for _, w := range m.warnings {
		b.WriteString(warningStyle.Render("⚠ " + w))
		b.WriteString("\n")
	}
	if len(m.warnings) > 0 {
		b.WriteString("\n")
	}
	if m.errMsg != "" {
		b.WriteString(errorStyle.Render("✗ " + m.errMsg))
		b.WriteString("\n\n")
	}
	if m.status != "" {
		b.WriteString(successStyle.Render("✓ " + m.status))
		b.WriteString("\n\n")
	}
	return b.String()
}

func (m Model) viewImport() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("ODD Profiles"))
	b.WriteString("\n\n")
	b.WriteString(warningStyle.Render("No oc-profiles store found at " + m.paths.StoreDir))
	b.WriteString("\n\n")
	b.WriteString(subtextStyle.Render("Import reads the profile agents from " + m.paths.OpencodeJSON()))
	b.WriteString("\n")
	b.WriteString(subtextStyle.Render("and keeps model assignments in its own store, so profiles survive `gentle-ai sync`."))
	b.WriteString("\n\n")
	b.WriteString(m.messages())
	b.WriteString(renderOptions([]string{"Import from opencode.json", "Quit"}, m.cursor))
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("j/k: navigate • enter: confirm • esc: quit"))
	return b.String()
}

func (m Model) viewMigrate() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("ODD Profiles"))
	b.WriteString("\n\n")
	b.WriteString(warningStyle.Render("This store holds gentle-ai 3.7 SDD profiles (schema v1)."))
	b.WriteString("\n\n")
	b.WriteString(subtextStyle.Render("Migration maps each profile onto ODD agents: orchestrator <- sdd-orchestrator,"))
	b.WriteString("\n")
	b.WriteString(subtextStyle.Render("explore <- sdd-explore, worker <- sdd-apply, verify <- sdd-verify, judges <- judges."))
	b.WriteString("\n")
	b.WriteString(subtextStyle.Render("The old store is backed up first; the next apply removes the retired SDD agents."))
	b.WriteString("\n\n")
	b.WriteString(m.messages())
	b.WriteString(renderOptions([]string{"Migrate SDD profiles to ODD", "Quit"}, m.cursor))
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("j/k: navigate • enter: confirm • esc: quit"))
	return b.String()
}

func (m Model) banner() string {
	if m.planErr != nil {
		return errorStyle.Render("✗ Cannot read opencode.json: "+m.planErr.Error()) + "\n\n"
	}
	if m.plan.CleanupPending {
		return warningStyle.Render("⚠ Retired SDD agents and files are pending removal. Press a to apply.") + "\n\n"
	}
	if n := m.plan.MissingCount(); n > 0 {
		return warningStyle.Render(fmt.Sprintf("⚠ %d agents are missing from opencode.json (gentle-ai sync may have removed them). Press a to apply.", n)) + "\n\n"
	}
	if !m.plan.InSync() {
		return warningStyle.Render("⚠ opencode.json is out of sync with the store. Press a to apply.") + "\n\n"
	}
	return ""
}

func (m Model) viewList() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("ODD Profiles"))
	b.WriteString("\n\n")
	b.WriteString(subtextStyle.Render("Your ODD model profiles for OpenCode. Each profile creates its own orchestrator (visible with Tab)."))
	b.WriteString("\n\n")
	b.WriteString(m.banner())
	b.WriteString(m.messages())

	profiles := m.store.Profiles.Profiles
	if len(profiles) == 0 {
		b.WriteString(subtextStyle.Render("No named profiles yet. Create one to assign different models per profile."))
		b.WriteString("\n\n")
	}
	options := make([]string, 0, len(profiles)+2)
	for _, p := range profiles {
		options = append(options, fmt.Sprintf("• %s ─── %s", p.Name, p.Get(core.RoleOrchestrator).Label("(no model assigned)")))
	}
	options = append(options, "Create new profile", "Quit")
	b.WriteString(renderOptions(options, m.cursor))
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("j/k: navigate • enter: edit • n: new • r: rename • d: delete • a: apply • s: sync • b: backups • esc: quit"))
	return b.String()
}

func (m Model) header() string {
	if m.editMode {
		return "Edit Profile"
	}
	return "Create SDD Profile"
}

func (m Model) viewName() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(m.header()))
	b.WriteString("\n\n")
	if m.editMode {
		b.WriteString(subtextStyle.Render("Profile: "))
		b.WriteString(selectedStyle.Render(m.draft.Name))
		b.WriteString("\n\n")
		b.WriteString(subtextStyle.Render("(Name cannot be changed when editing)"))
		b.WriteString("\n\n")
		b.WriteString(helpStyle.Render("enter: next • esc: back"))
		return b.String()
	}
	b.WriteString(headingStyle.Render("Profile name:"))
	b.WriteString("\n")
	b.WriteString(subtextStyle.Render("Use lowercase alphanumeric characters and hyphens only (slug format)"))
	b.WriteString("\n\n")
	b.WriteString(m.nameField())
	b.WriteString(helpStyle.Render("enter: next • esc: back"))
	return b.String()
}

// nameField renders the profile-name text input and its validation error.
func (m Model) nameField() string {
	var b, input strings.Builder
	for i, r := range m.nameInput {
		if i == m.namePos {
			input.WriteString(selectedStyle.Render("|"))
		}
		input.WriteRune(r)
	}
	if m.namePos == len(m.nameInput) {
		input.WriteString(selectedStyle.Render("|"))
	}
	b.WriteString(unselectedStyle.Render("  > "))
	b.WriteString(input.String())
	b.WriteString("\n\n")
	if m.nameErr != "" {
		b.WriteString(errorStyle.Render("✗ " + m.nameErr))
		b.WriteString("\n\n")
	}
	return b.String()
}

func (m Model) viewRename() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Rename Profile"))
	b.WriteString("\n\n")
	b.WriteString(subtextStyle.Render("Profile: "))
	b.WriteString(selectedStyle.Render(m.renameFrom))
	b.WriteString("\n\n")
	b.WriteString(headingStyle.Render("New name:"))
	b.WriteString("\n")
	b.WriteString(subtextStyle.Render("Use lowercase alphanumeric characters and hyphens only (slug format)"))
	b.WriteString("\n\n")
	b.WriteString(m.nameField())
	b.WriteString(helpStyle.Render("enter: next • esc: back"))
	return b.String()
}

func (m Model) viewRenameConfirm() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Rename Profile"))
	b.WriteString("\n\n")
	b.WriteString(warningStyle.Render(fmt.Sprintf("Rename profile %q to %q?", m.renameFrom, m.renameTo)))
	b.WriteString("\n\n")
	b.WriteString(subtextStyle.Render("Model assignments are kept. In opencode.json these agent keys are replaced:"))
	b.WriteString("\n\n")
	if i := m.store.Profiles.Find(m.renameFrom); i >= 0 {
		renamed := m.store.Profiles.Profiles[i].Clone()
		renamed.Name = m.renameTo
		oldKeys, newKeys := m.store.Profiles.Profiles[i].AgentKeys(), renamed.AgentKeys()
		for k := range oldKeys {
			b.WriteString(unselectedStyle.Render("  • " + oldKeys[k] + " → " + newKeys[k]))
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")
	b.WriteString(subtextStyle.Render("Sessions using the old orchestrator name will need to switch agents."))
	b.WriteString("\n\n")
	b.WriteString(renderOptions([]string{"Rename & Sync", "Cancel"}, m.cursor))
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("j/k: navigate • enter: confirm • esc: back"))
	return b.String()
}

func (m Model) viewPicker() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(m.header()))
	b.WriteString("\n\n")
	b.WriteString(headingStyle.Render("Assign Models"))
	b.WriteString("\n")
	b.WriteString(subtextStyle.Render("Assign models for profile: " + m.draft.Name))
	b.WriteString("\n\n")
	if m.errMsg != "" && m.picker.mode == modePhaseList {
		b.WriteString(errorStyle.Render("✗ " + m.errMsg))
		b.WriteString("\n\n")
	}
	b.WriteString(m.picker.render(m.assign, m.cursor))
	return b.String()
}

func (m Model) viewConfirm() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(m.header()))
	b.WriteString("\n\n")
	b.WriteString(headingStyle.Render("Profile Summary"))
	b.WriteString("\n\n")
	p := m.buildProfile()
	b.WriteString(subtextStyle.Render("Name: "))
	b.WriteString(selectedStyle.Render(p.Name))
	b.WriteString("\n")
	b.WriteString(subtextStyle.Render("Orchestrator: "))
	b.WriteString(unselectedStyle.Render(p.Get(core.RoleOrchestrator).Label("(default)")))
	b.WriteString("\n")
	assigned := 0
	for _, a := range p.Models {
		if a.IsSet() {
			assigned++
		}
	}
	if assigned > 0 {
		b.WriteString(subtextStyle.Render("Model assignments: "))
		b.WriteString(unselectedStyle.Render(fmt.Sprintf("%d assigned", assigned)))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	for _, r := range core.Roles()[1:] {
		fallback := "(default)"
		if r.Optional {
			fallback = "(global)"
		}
		b.WriteString(subtextStyle.Render(fmt.Sprintf("  %-14s ", r.Label)))
		b.WriteString(unselectedStyle.Render(p.Get(r.ID).Label(fallback)))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	label := "Create & Sync"
	if m.editMode {
		label = "Save & Sync"
	}
	b.WriteString(renderOptions([]string{label, "Cancel"}, m.cursor))
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("j/k: navigate • enter: confirm • esc: back"))
	return b.String()
}

func (m Model) viewDelete() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Delete Profile"))
	b.WriteString("\n\n")
	b.WriteString(warningStyle.Render(fmt.Sprintf("Are you sure you want to delete profile %q?", m.deleteName)))
	b.WriteString("\n\n")
	var keys []string
	if i := m.store.Profiles.Find(m.deleteName); i >= 0 {
		keys = m.store.Profiles.Profiles[i].AgentKeys()
	}
	b.WriteString(subtextStyle.Render(fmt.Sprintf("The following %d agent keys will be removed from opencode.json:", len(keys))))
	b.WriteString("\n\n")
	for _, k := range keys {
		b.WriteString(unselectedStyle.Render("  • " + k))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(warningStyle.Render("This action cannot be undone (a backup of opencode.json is written first)."))
	b.WriteString("\n\n")
	b.WriteString(renderOptions([]string{"Delete & Sync", "Cancel"}, m.cursor))
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("j/k: navigate • enter: confirm • esc: back"))
	return b.String()
}

func (m Model) viewBackups() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Backups"))
	b.WriteString("\n\n")
	b.WriteString(subtextStyle.Render("Snapshots in " + m.paths.BackupDir))
	b.WriteString("\n\n")
	b.WriteString(m.messages())
	if len(m.backups) == 0 {
		b.WriteString(subtextStyle.Render("No snapshots yet. Press s on the profile list to sync and write one."))
		b.WriteString("\n\n")
	}
	options := make([]string, 0, len(m.backups)+1)
	for _, info := range m.backups {
		status := fmt.Sprintf("%d profiles", info.Profiles)
		if !info.Valid {
			status = "CORRUPT"
		}
		cur := ""
		if info.Current {
			cur = " (current)"
		}
		options = append(options, fmt.Sprintf("#%-4d %-20s %s%s", info.Seq, info.CreatedAt, status, cur))
	}
	options = append(options, "← Back")
	b.WriteString(renderOptions(options, m.cursor))
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("j/k: navigate • enter: restore • esc: back"))
	return b.String()
}

func (m Model) viewRestoreConfirm() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Restore Backup"))
	b.WriteString("\n\n")
	b.WriteString(warningStyle.Render(fmt.Sprintf("Replace the store with snapshot #%d (%s, %d profiles)?", m.restoreInfo.Seq, m.restoreInfo.CreatedAt, m.restoreInfo.Profiles)))
	b.WriteString("\n\n")
	b.WriteString(subtextStyle.Render("The current store files are kept as *.replaced-<time>; opencode.json is synced afterwards."))
	b.WriteString("\n\n")
	b.WriteString(renderOptions([]string{"Restore & Sync", "Cancel"}, m.cursor))
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("j/k: navigate • enter: confirm • esc: back"))
	return b.String()
}
