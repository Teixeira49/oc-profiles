// Package tui implements the oc-profiles terminal UI. Screen layout, styles
// and keybindings follow the gentle-ai v3.7.0 "OpenCode SDD Profiles" menu
// (MIT, Gentleman-Programming/gentle-ai).
package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Teixeira49/oc-profiles/internal/backup"
	"github.com/Teixeira49/oc-profiles/internal/catalog"
	"github.com/Teixeira49/oc-profiles/internal/core"
)

type screen int

const (
	screenImport screen = iota
	screenMigrate
	screenList
	screenName
	screenPicker
	screenConfirm
	screenDelete
	screenRename
	screenRenameConfirm
	screenBackups
	screenRestoreConfirm
)

// CatalogLoader discovers models; injectable for tests.
type CatalogLoader func(ctx context.Context) (catalog.Catalog, error, error)

type catalogMsg struct {
	cat  catalog.Catalog
	err  error
	verr error
}

// Model is the root Bubble Tea model.
type Model struct {
	paths  core.Paths
	loader CatalogLoader

	screen screen
	cursor int

	store   *core.Store
	plan    core.ApplyResult
	planErr error
	status  string
	errMsg  string

	// create / edit
	editMode  bool
	draft     core.Profile
	nameInput []rune
	namePos   int
	nameErr   string
	assign    map[string]core.Assignment
	picker    pickerState

	deleteName string
	warnings   []string
	renameFrom string
	renameTo   string

	backups     []backup.Info
	restoreInfo backup.Info
}

// New builds the root model. When the store is missing the import screen is
// shown first.
func New(paths core.Paths, loader CatalogLoader) Model {
	m := Model{paths: paths, loader: loader}
	m.picker.status = catalogLoading
	var notes []string
	rep, err := backup.Recover(paths)
	if err != nil {
		m.errMsg = "Store recovery failed: " + err.Error()
	}
	if rep.Restored {
		notes = append(notes, fmt.Sprintf("Recovered the store from backup snapshot #%d (%s, %d profiles).", rep.From.Seq, rep.From.CreatedAt, rep.From.Profiles))
	}
	m.warnings = rep.Warnings
	if !core.StoreExists(paths) {
		m.screen = screenImport
		return m
	}
	s, err := core.LoadStore(paths)
	if errors.Is(err, core.ErrLegacyStore) {
		m.screen = screenMigrate
		return m
	}
	if err != nil {
		m.screen = screenImport
		m.errMsg = "Could not load store: " + err.Error()
		return m
	}
	m.store = s
	m.screen = screenList
	m.refreshPlan()
	// Auto-regenerate profile agents that a gentle-ai sync removed. The
	// one-time SDD cleanup is never run implicitly.
	if m.planErr == nil && m.plan.MissingCount() > 0 && !m.plan.CleanupPending {
		missing := m.plan.MissingCount()
		if res, err := m.store.Apply(); err != nil {
			m.errMsg = "Automatic recovery of missing agents failed: " + err.Error()
		} else {
			notes = append(notes, fmt.Sprintf("Restored %d missing profile agents: %s.", missing, strings.Join(res.Added, ", ")))
			notes = append(notes, m.snapshotNote())
		}
		m.refreshPlan()
	}
	m.status = strings.Join(notes, " ")
	return m
}

// snapshotNote mirrors the store and describes the result; a failure is
// reported in errMsg (loudly) without touching the store.
func (m *Model) snapshotNote() string {
	res, err := backup.Snapshot(m.paths)
	if err != nil {
		m.errMsg = "BACKUP FAILED (store and opencode.json are fine): " + err.Error()
		return ""
	}
	state := "unchanged"
	if res.Written {
		state = "new"
	}
	return fmt.Sprintf("Backup #%d (%s, %s).", res.Info.Seq, res.Info.CreatedAt, state)
}

func (m *Model) runSync() {
	res, err := backup.Sync(m.paths)
	if err != nil {
		m.errMsg = "Sync failed: " + err.Error()
		m.status = ""
		return
	}
	m.reloadStore()
	m.errMsg = ""
	parts := []string{"Synced: " + applySummary(res.Apply)}
	if res.Recovery.Restored {
		parts = append([]string{fmt.Sprintf("Recovered the store from snapshot #%d.", res.Recovery.From.Seq)}, parts...)
	}
	if res.BackupErr != nil {
		m.errMsg = "BACKUP FAILED (store and opencode.json are fine): " + res.BackupErr.Error()
	} else {
		state := "unchanged"
		if res.Snapshot.Written {
			state = "new"
		}
		parts = append(parts, fmt.Sprintf("Backup #%d (%s, %s).", res.Snapshot.Info.Seq, res.Snapshot.Info.CreatedAt, state))
	}
	m.warnings = append(res.Recovery.Warnings, res.Apply.Warnings...)
	m.status = strings.Join(parts, " ")
	m.refreshPlan()
}

// DefaultLoader runs `opencode models` and reads the variants cache.
func DefaultLoader(paths core.Paths) CatalogLoader {
	return func(ctx context.Context) (catalog.Catalog, error, error) {
		return catalog.Discover(ctx, os.Getenv(core.EnvOpencodeBin), paths.VariantsFile)
	}
}

// Init starts model discovery in the background.
func (m Model) Init() tea.Cmd {
	if m.loader == nil {
		return nil
	}
	loader := m.loader
	return func() tea.Msg {
		cat, err, verr := loader(context.Background())
		return catalogMsg{cat: cat, err: err, verr: verr}
	}
}

func (m *Model) refreshPlan() {
	if m.store == nil {
		return
	}
	m.plan, m.planErr = m.store.Plan()
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case catalogMsg:
		m.picker.catalog = msg.cat
		m.picker.err = msg.err
		if msg.err != nil {
			m.picker.status = catalogFailed
		} else {
			m.picker.status = catalogReady
		}
		if msg.verr != nil {
			m.picker.variantsW = "Effort variants unavailable: " + msg.verr.Error()
		}
		return m, nil
	case tea.KeyMsg:
		key := msg.String()
		if key == "ctrl+c" {
			return m, tea.Quit
		}
		switch m.screen {
		case screenImport:
			return m.updateImport(key)
		case screenMigrate:
			return m.updateMigrate(key)
		case screenList:
			return m.updateList(key)
		case screenName:
			return m.updateName(msg)
		case screenPicker:
			return m.updatePicker(key)
		case screenConfirm:
			return m.updateConfirm(key)
		case screenDelete:
			return m.updateDelete(key)
		case screenRename:
			return m.updateRename(msg)
		case screenRenameConfirm:
			return m.updateRenameConfirm(key)
		case screenBackups:
			return m.updateBackups(key)
		case screenRestoreConfirm:
			return m.updateRestoreConfirm(key)
		}
	}
	return m, nil
}

func navigate(key string, cursor, count int) int {
	switch key {
	case "up", "k":
		if cursor > 0 {
			cursor--
		}
	case "down", "j":
		if cursor < count-1 {
			cursor++
		}
	}
	return cursor
}

// --- import ---------------------------------------------------------------

func (m Model) updateImport(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "up", "k", "down", "j":
		m.cursor = navigate(key, m.cursor, 2)
	case "esc", "q":
		return m, tea.Quit
	case "enter":
		if m.cursor == 1 {
			return m, tea.Quit
		}
		s, res, err := core.Import(m.paths, core.ImportOptions{})
		if err != nil {
			m.errMsg = "Import failed: " + err.Error()
			return m, nil
		}
		m.store = s
		m.errMsg = ""
		m.status = fmt.Sprintf("Imported %d profiles from %s.", len(res.Profiles), m.paths.OpencodeJSON())
		if res.FromSDD {
			m.status += " Converted from SDD profiles; press a to apply."
		}
		m.status += " " + m.snapshotNote()
		m.screen = screenList
		m.cursor = 0
		m.refreshPlan()
	}
	return m, nil
}

// --- migrate --------------------------------------------------------------

func (m Model) updateMigrate(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "up", "k", "down", "j":
		m.cursor = navigate(key, m.cursor, 2)
	case "esc", "q":
		return m, tea.Quit
	case "enter":
		if m.cursor == 1 {
			return m, tea.Quit
		}
		res, err := core.Migrate(m.paths)
		if err != nil {
			m.errMsg = "Migration failed: " + err.Error()
			return m, nil
		}
		s, err := core.LoadStore(m.paths)
		if err != nil {
			m.errMsg = "Could not load migrated store: " + err.Error()
			return m, nil
		}
		m.store = s
		m.errMsg = ""
		m.status = fmt.Sprintf("Migrated %d profiles (old store backed up to %s). Press a to apply.", len(res.Profiles), res.Backup)
		m.status += " " + m.snapshotNote()
		m.screen = screenList
		m.cursor = 0
		m.refreshPlan()
	}
	return m, nil
}

// --- list -----------------------------------------------------------------

func (m Model) profileCount() int { return len(m.store.Profiles.Profiles) }

func (m Model) updateList(key string) (tea.Model, tea.Cmd) {
	count := m.profileCount() + 2
	switch key {
	case "up", "k", "down", "j":
		m.cursor = navigate(key, m.cursor, count)
	case "esc", "q":
		return m, tea.Quit
	case "n":
		m.startCreate()
	case "e":
		if m.cursor < m.profileCount() {
			m.startEdit(m.store.Profiles.Profiles[m.cursor])
		}
	case "a":
		m.runApply()
	case "s":
		m.runSync()
	case "b":
		m.backups = backup.List(m.paths.BackupDir)
		m.status, m.errMsg = "", ""
		m.screen = screenBackups
		m.cursor = 0
	case "r":
		if m.cursor < m.profileCount() {
			m.renameFrom = m.store.Profiles.Profiles[m.cursor].Name
			m.nameInput = []rune(m.renameFrom)
			m.namePos = len(m.nameInput)
			m.nameErr = ""
			m.status, m.errMsg = "", ""
			m.screen = screenRename
		}
	case "d":
		if m.cursor < m.profileCount() {
			m.deleteName = m.store.Profiles.Profiles[m.cursor].Name
			m.screen = screenDelete
			m.cursor = 1 // default to Cancel
		}
	case "enter":
		switch {
		case m.cursor < m.profileCount():
			m.startEdit(m.store.Profiles.Profiles[m.cursor])
		case m.cursor == m.profileCount():
			m.startCreate()
		default:
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *Model) runApply() {
	res, err := m.store.Apply()
	if err != nil {
		m.errMsg = "Apply failed: " + err.Error()
		m.status = ""
		return
	}
	m.errMsg = ""
	m.status = applySummary(res)
	if !res.InSync() {
		m.status += " " + m.snapshotNote()
	}
	m.refreshPlan()
}

func applySummary(res core.ApplyResult) string {
	if !res.Changed && len(res.RemovedFiles) == 0 {
		return "opencode.json is already in sync."
	}
	parts := []string{}
	if n := len(res.Added); n > 0 {
		parts = append(parts, fmt.Sprintf("%d added", n))
	}
	if n := len(res.Updated); n > 0 {
		parts = append(parts, fmt.Sprintf("%d updated", n))
	}
	if n := len(res.Removed); n > 0 {
		parts = append(parts, fmt.Sprintf("%d removed", n))
	}
	if n := len(res.RemovedFiles); n > 0 {
		parts = append(parts, fmt.Sprintf("%d SDD files removed", n))
	}
	if n := len(res.Warnings); n > 0 {
		parts = append(parts, fmt.Sprintf("%d warnings (run `oc-profiles apply --dry-run` for details)", n))
	}
	msg := "Applied: " + strings.Join(parts, ", ") + "."
	if res.Backup != "" {
		msg += " Backup: " + res.Backup
	}
	return msg
}

// --- create / edit ----------------------------------------------------------

func (m *Model) startCreate() {
	m.editMode = false
	m.draft = core.Profile{Models: map[string]core.Assignment{}}
	m.nameInput = nil
	m.namePos = 0
	m.nameErr = ""
	m.assign = map[string]core.Assignment{}
	m.picker.resetSub()
	m.picker.initAllPhases(m.assign)
	m.status, m.errMsg = "", ""
	m.screen = screenName
	m.cursor = 0
}

func (m *Model) startEdit(p core.Profile) {
	m.editMode = true
	m.draft = p.Clone()
	m.nameInput = []rune(p.Name)
	m.namePos = len(m.nameInput)
	m.nameErr = ""
	m.assign = map[string]core.Assignment{}
	for _, r := range core.Roles() {
		if a := p.Get(r.ID); a.IsSet() {
			m.assign[r.ID] = a
		}
	}
	m.picker.resetSub()
	m.picker.initAllPhases(m.assign)
	m.status, m.errMsg = "", ""
	m.screen = screenName
	m.cursor = 0
}

func (m Model) updateName(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch key {
	case "esc":
		m.screen = screenList
		m.cursor = 0
		return m, nil
	case "enter":
		if !m.editMode {
			name := strings.ToLower(strings.TrimSpace(string(m.nameInput)))
			if err := core.ValidateProfileName(name); err != nil {
				m.nameErr = err.Error()
				return m, nil
			}
			if m.store.Profiles.Find(name) >= 0 {
				m.nameErr = fmt.Sprintf("profile %q already exists", name)
				return m, nil
			}
			m.draft.Name = name
		}
		m.nameErr = ""
		m.screen = screenPicker
		m.cursor = 0
		return m, nil
	}
	if m.editMode {
		return m, nil
	}
	m.editName(msg)
	return m, nil
}

// editName applies a key press to the profile-name text input.
func (m *Model) editName(msg tea.KeyMsg) {
	switch msg.String() {
	case "backspace":
		if m.namePos > 0 {
			m.nameInput = append(m.nameInput[:m.namePos-1], m.nameInput[m.namePos:]...)
			m.namePos--
		}
	case "delete":
		if m.namePos < len(m.nameInput) {
			m.nameInput = append(m.nameInput[:m.namePos], m.nameInput[m.namePos+1:]...)
		}
	case "left":
		if m.namePos > 0 {
			m.namePos--
		}
	case "right":
		if m.namePos < len(m.nameInput) {
			m.namePos++
		}
	case "home", "ctrl+a":
		m.namePos = 0
	case "end", "ctrl+e":
		m.namePos = len(m.nameInput)
	default:
		if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace {
			for _, r := range msg.Runes {
				r = []rune(strings.ToLower(string(r)))[0]
				m.nameInput = append(m.nameInput[:m.namePos], append([]rune{r}, m.nameInput[m.namePos:]...)...)
				m.namePos++
			}
			m.nameErr = ""
		}
	}
}

// --- backups --------------------------------------------------------------

func (m Model) updateBackups(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "up", "k", "down", "j":
		m.cursor = navigate(key, m.cursor, len(m.backups)+1)
	case "esc", "q":
		m.screen = screenList
		m.cursor = 0
	case "enter":
		if m.cursor >= len(m.backups) {
			m.screen = screenList
			m.cursor = 0
			return m, nil
		}
		info := m.backups[m.cursor]
		if !info.Valid {
			m.errMsg = fmt.Sprintf("Snapshot #%d is corrupt and cannot be restored.", info.Seq)
			return m, nil
		}
		m.errMsg = ""
		m.restoreInfo = info
		m.screen = screenRestoreConfirm
		m.cursor = 1 // default to Cancel
	}
	return m, nil
}

func (m Model) updateRestoreConfirm(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "up", "k", "down", "j":
		m.cursor = navigate(key, m.cursor, 2)
	case "esc":
		m.screen = screenBackups
		m.cursor = 0
	case "enter":
		if m.cursor == 0 {
			if _, _, err := backup.RestoreSeq(m.paths, m.restoreInfo.Seq); err != nil {
				m.errMsg = "Restore failed: " + err.Error()
			} else {
				m.reloadStore()
				m.runSync()
				m.status = fmt.Sprintf("Restored snapshot #%d. ", m.restoreInfo.Seq) + m.status
			}
		}
		m.screen = screenList
		m.cursor = 0
	}
	return m, nil
}

// --- rename ---------------------------------------------------------------

func (m Model) updateRename(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.screen = screenList
		return m, nil
	case "enter":
		name := strings.ToLower(strings.TrimSpace(string(m.nameInput)))
		if name == m.renameFrom {
			m.status = "Name unchanged."
			m.screen = screenList
			return m, nil
		}
		if err := core.ValidateProfileName(name); err != nil {
			m.nameErr = err.Error()
			return m, nil
		}
		if m.store.Profiles.Find(name) >= 0 {
			m.nameErr = fmt.Sprintf("profile %q already exists", name)
			return m, nil
		}
		m.renameTo = name
		m.nameErr = ""
		m.screen = screenRenameConfirm
		m.cursor = 0
		return m, nil
	}
	m.editName(msg)
	return m, nil
}

func (m Model) updateRenameConfirm(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "up", "k", "down", "j":
		m.cursor = navigate(key, m.cursor, 2)
	case "esc":
		m.screen = screenRename
	case "enter":
		if m.cursor == 0 {
			previous, _, readErr := core.ReadStoreFiles(m.paths)
			if err := m.store.Profiles.Rename(m.renameFrom, m.renameTo); err != nil {
				m.errMsg = err.Error()
			} else if readErr != nil {
				m.errMsg = "Could not read the store: " + readErr.Error()
				m.reloadStore()
			} else if err := m.saveAndApply(fmt.Sprintf("Profile %q renamed to %q.", m.renameFrom, m.renameTo)); err != nil {
				// Apply failed: undo the rename so store and opencode.json agree.
				if rbErr := core.RestoreProfilesFile(m.paths, previous); rbErr != nil {
					m.errMsg = fmt.Sprintf("Apply failed (%v) and rolling back the rename failed: %v", err, rbErr)
				} else {
					m.errMsg = fmt.Sprintf("Apply failed, rename rolled back: %v", err)
				}
				m.reloadStore()
				m.renameTo = m.renameFrom
				m.refreshPlan()
			}
		}
		m.screen = screenList
		m.cursor = max(0, m.store.Profiles.Find(m.renameTo))
	}
	return m, nil
}

func (m Model) updatePicker(key string) (tea.Model, tea.Cmd) {
	if m.picker.mode != modePhaseList {
		m.picker.handleSubNav(key, m.assign)
		return m, nil
	}
	rows := pickerRows()
	count := pickerOptionCount()
	switch key {
	case "up", "k":
		c := navigate(key, m.cursor, count)
		if c < len(rows) && rows[c].kind == rowSeparator && c > 0 {
			c--
		}
		m.cursor = c
	case "down", "j":
		c := navigate(key, m.cursor, count)
		if c < len(rows) && rows[c].kind == rowSeparator && c < count-1 {
			c++
		}
		m.cursor = c
	case "backspace", "delete":
		m.picker.clearRow(m.assign, m.cursor)
	case "esc":
		m.screen = screenName
		m.cursor = 0
	case "enter":
		switch {
		case m.cursor == len(rows): // Continue
			m.screen = screenConfirm
			m.cursor = 0
		case m.cursor == len(rows)+1: // Back
			m.screen = screenName
			m.cursor = 0
		case rows[m.cursor].kind == rowSeparator:
		default:
			switch m.picker.status {
			case catalogLoading:
				m.errMsg = "Models are still loading from OpenCode, try again in a moment."
				return m, nil
			case catalogFailed:
				m.errMsg = "Model catalog unavailable; cannot change models."
				return m, nil
			}
			if len(m.picker.catalog.Providers) == 0 {
				m.errMsg = "OpenCode reported no models."
				return m, nil
			}
			m.errMsg = ""
			m.picker.selectedRow = m.cursor
			m.picker.mode = modeProviderSelect
			m.picker.providerCursor, m.picker.providerScroll = 0, 0
			if cur := m.currentRowAssignment(m.cursor); cur.IsSet() {
				m.picker.focusProvider(cur.Provider())
			}
		}
	}
	return m, nil
}

func (m Model) currentRowAssignment(idx int) core.Assignment {
	rows := pickerRows()
	switch rows[idx].kind {
	case rowSetAll:
		return m.picker.allPhases
	case rowAgent:
		return m.assign[rows[idx].role]
	}
	return core.Assignment{}
}

// focusProvider moves the provider cursor onto id when it exists.
func (s *pickerState) focusProvider(id string) {
	for i, p := range s.catalog.ProviderIDs() {
		if p == id {
			s.providerCursor = i
			if i >= maxVisibleItems {
				s.providerScroll = i - maxVisibleItems + 1
			}
			return
		}
	}
}

func (m Model) buildProfile() core.Profile {
	p := core.Profile{Name: m.draft.Name, Models: map[string]core.Assignment{}}
	for _, r := range core.Roles() {
		p.Set(r.ID, m.assign[r.ID])
	}
	return p
}

func (m Model) updateConfirm(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "up", "k", "down", "j":
		m.cursor = navigate(key, m.cursor, 2)
	case "esc":
		m.screen = screenPicker
		m.cursor = 0
	case "enter":
		if m.cursor == 1 {
			m.screen = screenList
			m.cursor = 0
			return m, nil
		}
		p := m.buildProfile()
		m.store.Profiles.Upsert(p)
		m.saveAndApply(fmt.Sprintf("Profile %q saved.", p.Name))
		m.screen = screenList
		m.cursor = max(0, m.store.Profiles.Find(p.Name))
	}
	return m, nil
}

// saveAndApply saves the store, applies it and mirrors it. It returns the
// apply error (if any) so callers can roll back.
func (m *Model) saveAndApply(okMsg string) error {
	if err := m.store.SaveProfiles(); err != nil {
		m.errMsg = "Could not save store: " + err.Error()
		m.status = ""
		m.reloadStore()
		return err
	}
	res, err := m.store.Apply()
	if err != nil {
		m.errMsg = okMsg + " But apply failed: " + err.Error() + " (run `oc-profiles apply`)"
		m.status = ""
		m.snapshotNote()
		m.refreshPlan()
		return err
	}
	note := m.snapshotNote()
	if !strings.HasPrefix(m.errMsg, "BACKUP FAILED") {
		m.errMsg = ""
	}
	m.status = okMsg + " " + applySummary(res) + " " + note
	m.refreshPlan()
	return nil
}

func (m *Model) reloadStore() {
	if s, err := core.LoadStore(m.paths); err == nil {
		m.store = s
	}
}

func (m Model) updateDelete(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "up", "k", "down", "j":
		m.cursor = navigate(key, m.cursor, 2)
	case "esc":
		m.screen = screenList
		m.cursor = 0
	case "enter":
		if m.cursor == 0 {
			m.store.Profiles.Remove(m.deleteName)
			m.saveAndApply(fmt.Sprintf("Profile %q deleted.", m.deleteName))
		}
		m.screen = screenList
		m.cursor = 0
	}
	return m, nil
}

// Run starts the TUI.
func Run(paths core.Paths) error {
	p := tea.NewProgram(New(paths, DefaultLoader(paths)), tea.WithAltScreen())
	_, err := p.Run()
	if errors.Is(err, tea.ErrProgramKilled) {
		return nil
	}
	return err
}
