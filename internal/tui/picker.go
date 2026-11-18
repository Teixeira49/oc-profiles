package tui

// The model picker mirrors gentle-ai v3.7.0 internal/tui/screens/model_picker.go
// (MIT, Gentleman-Programming/gentle-ai): phase list -> provider -> model
// (type-to-search) -> reasoning effort.

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/teixeira49/oc-profiles/internal/catalog"
	"github.com/teixeira49/oc-profiles/internal/core"
)

type pickerMode int

const (
	modePhaseList pickerMode = iota
	modeProviderSelect
	modeModelSelect
	modeEffortSelect
)

type catalogStatus int

const (
	catalogLoading catalogStatus = iota
	catalogReady
	catalogFailed
)

const (
	maxVisibleItems     = 10
	maxVisiblePhaseRows = 16
)

type rowKind int

const (
	rowAgent rowKind = iota
	rowSetAll
	rowSeparator
)

type pickerRow struct {
	kind  rowKind
	label string
	role  string
}

// pickerRows returns the profile picker rows: orchestrator, "Set all
// subagents", explore/worker/verify, then the Judgment Day judges.
func pickerRows() []pickerRow {
	var rows []pickerRow
	for _, r := range core.Roles() {
		switch {
		case r.ID == core.RoleExplore:
			rows = append(rows, pickerRow{kind: rowSetAll, label: "Set all subagents"})
		case r.ID == core.RoleJudgeA:
			rows = append(rows, pickerRow{kind: rowSeparator, label: "--- Judgment Day ---"})
		}
		rows = append(rows, pickerRow{kind: rowAgent, label: r.Label, role: r.ID})
	}
	return rows
}

// pickerOptionCount is rows + Continue + Back.
func pickerOptionCount() int { return len(pickerRows()) + 2 }

type pickerState struct {
	catalog   catalog.Catalog
	status    catalogStatus
	err       error
	variantsW string

	mode        pickerMode
	selectedRow int
	provider    string

	providerCursor, providerScroll int
	modelCursor, modelScroll       int
	search                         string
	effortCursor, effortScroll     int

	pending      core.Assignment
	effortLevels []string

	// allPhases tracks the last value set through "Set all subagents".
	allPhases core.Assignment
}

func (s *pickerState) resetSub() {
	s.mode = modePhaseList
	s.providerCursor, s.providerScroll = 0, 0
	s.modelCursor, s.modelScroll = 0, 0
	s.search = ""
	s.effortCursor, s.effortScroll = 0, 0
	s.pending = core.Assignment{}
	s.effortLevels = nil
}

// initAllPhases shows the shared model on the "Set all" row when every
// subagent already uses the same assignment.
func (s *pickerState) initAllPhases(assign map[string]core.Assignment) {
	s.allPhases = core.Assignment{}
	var first core.Assignment
	for i, p := range core.SubagentRoleIDs() {
		a := assign[p]
		if !a.IsSet() {
			return
		}
		if i == 0 {
			first = a
		} else if a != first {
			return
		}
	}
	s.allPhases = first
}

func (s pickerState) filteredModels() []string {
	models := s.catalog.Providers[s.provider]
	q := strings.ToLower(strings.TrimSpace(s.search))
	if q == "" {
		return models
	}
	var out []string
	for _, m := range models {
		if strings.Contains(strings.ToLower(m), q) {
			out = append(out, m)
		}
	}
	return out
}

func effortOptions(levels []string) []string {
	opts := []string{"default"}
	for _, l := range levels {
		if l != "default" {
			opts = append(opts, l)
		}
	}
	return opts
}

// applyToRow writes a into the assignments for the selected row.
func (s *pickerState) applyToRow(assign map[string]core.Assignment, a core.Assignment) {
	rows := pickerRows()
	if s.selectedRow < 0 || s.selectedRow >= len(rows) {
		return
	}
	row := rows[s.selectedRow]
	switch row.kind {
	case rowSetAll:
		for _, p := range core.SubagentRoleIDs() {
			assign[p] = a
		}
		s.allPhases = a
	case rowAgent:
		assign[row.role] = a
	}
}

// clearRow removes the assignment for the row at idx.
func (s *pickerState) clearRow(assign map[string]core.Assignment, idx int) {
	rows := pickerRows()
	if idx < 0 || idx >= len(rows) {
		return
	}
	row := rows[idx]
	switch row.kind {
	case rowSetAll:
		for _, p := range core.SubagentRoleIDs() {
			delete(assign, p)
		}
		s.allPhases = core.Assignment{}
	case rowAgent:
		delete(assign, row.role)
	}
}

// handleSubNav processes keys inside provider/model/effort sub-modes.
func (s *pickerState) handleSubNav(key string, assign map[string]core.Assignment) bool {
	switch s.mode {
	case modeProviderSelect:
		ids := s.catalog.ProviderIDs()
		switch key {
		case "up", "k":
			if s.providerCursor > 0 {
				s.providerCursor--
				if s.providerCursor < s.providerScroll {
					s.providerScroll = s.providerCursor
				}
			}
		case "down", "j":
			if s.providerCursor < len(ids)-1 {
				s.providerCursor++
				if s.providerCursor >= s.providerScroll+maxVisibleItems {
					s.providerScroll = s.providerCursor - maxVisibleItems + 1
				}
			}
		case "enter":
			if len(ids) == 0 {
				return true
			}
			s.provider = ids[s.providerCursor]
			s.mode = modeModelSelect
			s.modelCursor, s.modelScroll, s.search = 0, 0, ""
		case "esc":
			s.resetSub()
		}
		return true

	case modeModelSelect:
		models := s.filteredModels()
		switch key {
		case "up", "k":
			if s.modelCursor > 0 {
				s.modelCursor--
				if s.modelCursor < s.modelScroll {
					s.modelScroll = s.modelCursor
				}
			}
		case "down", "j":
			if s.modelCursor < len(models)-1 {
				s.modelCursor++
				if s.modelCursor >= s.modelScroll+maxVisibleItems {
					s.modelScroll = s.modelCursor - maxVisibleItems + 1
				}
			}
		case "enter":
			if len(models) == 0 {
				return true
			}
			a := core.Assignment{Model: s.provider + "/" + models[s.modelCursor]}
			if levels := s.catalog.Efforts(s.provider, models[s.modelCursor]); len(levels) > 0 {
				s.pending = a
				s.effortLevels = levels
				s.mode = modeEffortSelect
				s.effortCursor, s.effortScroll = 0, 0
				return true
			}
			s.applyToRow(assign, a)
			s.resetSub()
		case "backspace":
			if s.search != "" {
				r := []rune(s.search)
				s.search = string(r[:len(r)-1])
				s.modelCursor, s.modelScroll = 0, 0
			}
		case "ctrl+u":
			s.search = ""
			s.modelCursor, s.modelScroll = 0, 0
		case "esc":
			s.mode = modeProviderSelect
			s.modelCursor, s.modelScroll, s.search = 0, 0, ""
		default:
			if isSearchInput(key) {
				s.search += key
				s.modelCursor, s.modelScroll = 0, 0
			}
		}
		return true

	case modeEffortSelect:
		opts := effortOptions(s.effortLevels)
		switch key {
		case "up", "k":
			if s.effortCursor > 0 {
				s.effortCursor--
				if s.effortCursor < s.effortScroll {
					s.effortScroll = s.effortCursor
				}
			}
		case "down", "j":
			if s.effortCursor < len(opts)-1 {
				s.effortCursor++
				if s.effortCursor >= s.effortScroll+maxVisibleItems {
					s.effortScroll = s.effortCursor - maxVisibleItems + 1
				}
			}
		case "enter":
			a := s.pending
			if e := opts[s.effortCursor]; e != "default" {
				a.Variant = e
			}
			s.applyToRow(assign, a)
			s.resetSub()
		case "esc":
			s.mode = modeModelSelect
			s.effortCursor, s.effortScroll = 0, 0
			s.pending = core.Assignment{}
			s.effortLevels = nil
		}
		return true
	}
	return false
}

func isSearchInput(key string) bool {
	r := []rune(key)
	return len(r) == 1 && unicode.IsPrint(r[0]) && r[0] != 'j' && r[0] != 'k'
}

func formatAssignmentLabel(row string, a core.Assignment) string {
	if a.Variant != "" {
		return fmt.Sprintf("%-20s %s / %s [%s]", row, a.Provider(), a.ModelID(), a.Variant)
	}
	return fmt.Sprintf("%-20s %s / %s", row, a.Provider(), a.ModelID())
}

func (s pickerState) render(assign map[string]core.Assignment, cursor int) string {
	switch s.mode {
	case modeProviderSelect:
		return s.renderProviderSelect()
	case modeModelSelect:
		return s.renderModelSelect()
	case modeEffortSelect:
		return s.renderEffortSelect()
	}
	return s.renderPhaseList(assign, cursor)
}

func (s pickerState) renderPhaseList(assign map[string]core.Assignment, cursor int) string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Assign Models to ODD Agents"))
	b.WriteString("\n\n")

	switch s.status {
	case catalogLoading:
		b.WriteString(warningStyle.Render("Discovering models from OpenCode..."))
		b.WriteString("\n\n")
	case catalogFailed:
		b.WriteString(warningStyle.Render("Could not discover models from OpenCode."))
		b.WriteString("\n")
		if s.err != nil {
			b.WriteString(subtextStyle.Render(s.err.Error()))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	if s.variantsW != "" {
		b.WriteString(warningStyle.Render(s.variantsW))
		b.WriteString("\n\n")
	}

	b.WriteString(subtextStyle.Render("Current assignments:"))
	b.WriteString("\n\n")

	rows := pickerRows()
	start, end := 0, len(rows)
	if len(rows) > maxVisiblePhaseRows {
		start = max(0, min(cursor-maxVisiblePhaseRows+1, len(rows)-maxVisiblePhaseRows))
		end = start + maxVisiblePhaseRows
	}
	if start > 0 {
		b.WriteString(subtextStyle.Render("  ↑ more assignments") + "\n")
	}
	for idx := start; idx < end; idx++ {
		row := rows[idx]
		focused := idx == cursor
		var label string
		switch row.kind {
		case rowSeparator:
			prefix := "  "
			if focused {
				prefix = cursorPrefix
			}
			b.WriteString(subtextStyle.Render(prefix+row.label) + "\n")
			continue
		case rowSetAll:
			if s.allPhases.IsSet() {
				label = formatAssignmentLabel(row.label, s.allPhases)
			} else {
				label = fmt.Sprintf("%-20s (not set)", row.label)
			}
		default:
			name := row.label
			if row.role == core.RoleOrchestrator {
				name += " (coordinator)"
			}
			if a := assign[row.role]; a.IsSet() {
				label = formatAssignmentLabel(name, a)
			} else if r, _ := core.RoleByID(row.role); r.Optional {
				label = fmt.Sprintf("%-20s (global)", name)
			} else {
				label = fmt.Sprintf("%-20s (default)", name)
			}
		}
		if focused {
			b.WriteString(selectedStyle.Render(cursorPrefix+label) + "\n")
		} else {
			b.WriteString(unselectedStyle.Render("  "+label) + "\n")
		}
	}
	if end < len(rows) {
		b.WriteString(subtextStyle.Render("  ↓ more assignments") + "\n")
	}

	b.WriteString("\n")
	b.WriteString(renderOptions([]string{"Continue", "← Back"}, cursor-len(rows)))
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("j/k: navigate • enter: change model / confirm • backspace: clear • esc: back"))
	return b.String()
}

func (s pickerState) renderProviderSelect() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Select provider:"))
	b.WriteString("\n\n")
	ids := s.catalog.ProviderIDs()
	end := min(s.providerScroll+maxVisibleItems, len(ids))
	if s.providerScroll > 0 {
		b.WriteString(subtextStyle.Render("  ↑ more") + "\n")
	}
	for i := s.providerScroll; i < end; i++ {
		label := fmt.Sprintf("%s (%d models)", ids[i], len(s.catalog.Providers[ids[i]]))
		if i == s.providerCursor {
			b.WriteString(selectedStyle.Render(cursorPrefix+label) + "\n")
		} else {
			b.WriteString(unselectedStyle.Render("  "+label) + "\n")
		}
	}
	if end < len(ids) {
		b.WriteString(subtextStyle.Render("  ↓ more") + "\n")
	}
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("j/k: navigate • enter: select • esc: back"))
	return b.String()
}

func (s pickerState) renderModelSelect() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(fmt.Sprintf("Select model (%s):", s.provider)))
	b.WriteString("\n\n")
	search := "_"
	if s.search != "" {
		search = s.search + "_"
	}
	b.WriteString(subtextStyle.Render("Search: " + search))
	b.WriteString("\n\n")

	models := s.filteredModels()
	if len(models) == 0 {
		b.WriteString(warningStyle.Render("  No models match your search."))
		b.WriteString("\n\n")
		b.WriteString(helpStyle.Render("type: search • backspace: delete • ctrl+u: clear • esc: back"))
		return b.String()
	}
	end := min(s.modelScroll+maxVisibleItems, len(models))
	if s.modelScroll > 0 {
		b.WriteString(subtextStyle.Render("  ↑ more") + "\n")
	}
	for i := s.modelScroll; i < end; i++ {
		label := models[i]
		if len(s.catalog.Efforts(s.provider, models[i])) > 0 {
			label += subtextStyle.Render("  (effort)")
		}
		if i == s.modelCursor {
			b.WriteString(selectedStyle.Render(cursorPrefix+label) + "\n")
		} else {
			b.WriteString(unselectedStyle.Render("  "+label) + "\n")
		}
	}
	if end < len(models) {
		b.WriteString(subtextStyle.Render("  ↓ more") + "\n")
	}
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("j/k: navigate • type: search • backspace: delete • ctrl+u: clear • enter: select • esc: back"))
	return b.String()
}

func (s pickerState) renderEffortSelect() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Select reasoning effort level:"))
	b.WriteString("\n\n")
	opts := effortOptions(s.effortLevels)
	end := min(s.effortScroll+maxVisibleItems, len(opts))
	if s.effortScroll > 0 {
		b.WriteString(subtextStyle.Render("  ↑ more") + "\n")
	}
	for i := s.effortScroll; i < end; i++ {
		if i == s.effortCursor {
			b.WriteString(selectedStyle.Render(cursorPrefix+opts[i]) + "\n")
		} else {
			b.WriteString(unselectedStyle.Render("  "+opts[i]) + "\n")
		}
	}
	if end < len(opts) {
		b.WriteString(subtextStyle.Render("  ↓ more") + "\n")
	}
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("j/k: navigate • enter: select • esc: back"))
	return b.String()
}
