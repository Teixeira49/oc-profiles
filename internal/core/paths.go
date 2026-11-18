package core

import (
	"os"
	"path/filepath"
)

// Environment variables that override default locations. They make every
// path injectable so tests and dry runs never touch the real home directory.
const (
	EnvOpencodeDir  = "OC_PROFILES_OPENCODE_DIR"
	EnvHome         = "OC_PROFILES_HOME"
	EnvVariantsFile = "OC_PROFILES_VARIANTS_FILE"
	EnvOpencodeBin  = "OC_PROFILES_OPENCODE_BIN"
	EnvBackupDir    = "OC_PROFILES_BACKUP_DIR"
)

// Paths groups every filesystem location the tool reads or writes.
type Paths struct {
	OpencodeDir  string // ~/.config/opencode
	StoreDir     string // ~/.config/oc-profiles
	VariantsFile string // ~/.gentle-ai/cache/model-variants.json
	BackupDir    string // ~/.local/share/oc-profiles/backup (independent mirror)
}

// DefaultPaths resolves locations from the environment, falling back to the
// standard locations under the user's home directory.
func DefaultPaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}
	p := Paths{
		OpencodeDir:  filepath.Join(home, ".config", "opencode"),
		StoreDir:     filepath.Join(home, ".config", "oc-profiles"),
		VariantsFile: filepath.Join(home, ".gentle-ai", "cache", "model-variants.json"),
		BackupDir:    filepath.Join(home, ".local", "share", "oc-profiles", "backup"),
	}
	if v := os.Getenv(EnvOpencodeDir); v != "" {
		p.OpencodeDir = v
	}
	if v := os.Getenv(EnvHome); v != "" {
		p.StoreDir = v
	}
	if v := os.Getenv(EnvVariantsFile); v != "" {
		p.VariantsFile = v
	}
	if v := os.Getenv(EnvBackupDir); v != "" {
		p.BackupDir = v
	}
	return p, nil
}

// OpencodeJSON is the path of opencode.json.
func (p Paths) OpencodeJSON() string { return filepath.Join(p.OpencodeDir, "opencode.json") }

// ProfilesFile is the path of the store's profiles.json.
func (p Paths) ProfilesFile() string { return filepath.Join(p.StoreDir, "profiles.json") }

// TemplatesFile is the path of the store's templates.json.
func (p Paths) TemplatesFile() string { return filepath.Join(p.StoreDir, "templates.json") }

// LegacyDir holds v1 (SDD) artifacts kept by `migrate` until the SDD cleanup
// has been applied.
func (p Paths) LegacyDir() string { return filepath.Join(p.StoreDir, "legacy") }

// legacyAssetsDir is the v1 snapshot of SDD prompts and skills.
func (p Paths) legacyAssetsDir() string { return filepath.Join(p.LegacyDir(), "assets") }

// legacyTemplatesFile is the v1 templates.json (base agents snapshot).
func (p Paths) legacyTemplatesFile() string { return filepath.Join(p.LegacyDir(), "templates-v1.json") }
