package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/Teixeira49/oc-profiles/internal/ojson"
)

// StoreVersion is the current profiles.json / templates.json schema.
// Version 1 held gentle-ai 3.7 SDD profiles; version 2 holds ODD profiles.
const StoreVersion = 2

// ErrStoreExists is returned by Import when a store already exists.
var ErrStoreExists = errors.New("store already exists (use --force to overwrite)")

// ErrNewerStore is returned for a store written by a newer oc-profiles.
var ErrNewerStore = errors.New("store was written by a newer oc-profiles")

// ErrLegacyStore is returned by LoadStore for a v1 (SDD) store.
var ErrLegacyStore = errors.New("store holds gentle-ai 3.7 SDD profiles (schema v1); run `oc-profiles migrate`")

// Store is the oc-profiles source of truth.
type Store struct {
	Paths    Paths
	Profiles Profiles
	// Sources is the last good snapshot of the live gentle-ai agents used as
	// templates; it is only used when an agent is missing from opencode.json.
	Sources *ojson.Object
}

// StoreExists reports whether profiles.json is present.
func StoreExists(p Paths) bool { return fileExists(p.ProfilesFile()) }

// StoreVersionOf returns the schema version of an existing profiles.json.
func StoreVersionOf(p Paths) (int, error) {
	data, err := os.ReadFile(p.ProfilesFile())
	if err != nil {
		return 0, err
	}
	var head struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return 0, fmt.Errorf("parse %s: %w", p.ProfilesFile(), err)
	}
	if head.Version == 0 {
		head.Version = 1
	}
	return head.Version, nil
}

// LoadStore reads and validates a v2 store.
func LoadStore(p Paths) (*Store, error) {
	v, err := StoreVersionOf(p)
	if err != nil {
		return nil, err
	}
	if v < StoreVersion {
		return nil, ErrLegacyStore
	}
	if v > StoreVersion {
		return nil, fmt.Errorf("%w (schema v%d, this build reads v%d)", ErrNewerStore, v, StoreVersion)
	}
	profilesRaw, templatesRaw, err := ReadStoreFiles(p)
	if err != nil {
		return nil, err
	}
	s, err := ParseStorePayload(p, profilesRaw, templatesRaw)
	if err != nil {
		return nil, fmt.Errorf("invalid store in %s: %w", p.StoreDir, err)
	}
	return s, nil
}

// MarshalProfiles returns the bytes SaveProfiles writes.
func (s *Store) MarshalProfiles() ([]byte, error) {
	s.Profiles.Version = StoreVersion
	s.Profiles.Sort()
	data, err := json.MarshalIndent(s.Profiles, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// MarshalTemplates returns the bytes SaveTemplates writes.
func (s *Store) MarshalTemplates() ([]byte, error) {
	doc := ojson.NewObject()
	doc.Set("version", json.Number(fmt.Sprint(StoreVersion)))
	doc.Set("sources", s.Sources)
	data, err := ojson.Marshal(doc)
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// SaveProfiles writes profiles.json atomically.
func (s *Store) SaveProfiles() error {
	data, err := s.MarshalProfiles()
	if err != nil {
		return err
	}
	return writeFileAtomic(s.Paths.ProfilesFile(), data, 0o644)
}

// SaveTemplates writes templates.json (the source snapshot) atomically.
func (s *Store) SaveTemplates() error {
	data, err := s.MarshalTemplates()
	if err != nil {
		return err
	}
	return writeFileAtomic(s.Paths.TemplatesFile(), data, 0o644)
}

// Save writes both store files.
func (s *Store) Save() error {
	if err := os.MkdirAll(s.Paths.StoreDir, 0o755); err != nil {
		return err
	}
	if err := s.SaveTemplates(); err != nil {
		return err
	}
	return s.SaveProfiles()
}

// snapshotSources copies the live gentle-ai source agents found in agents.
func snapshotSources(agents *ojson.Object) *ojson.Object {
	out := ojson.NewObject()
	for _, name := range SourceNames() {
		if v, ok := agents.GetObject(name); ok {
			out.Set(name, ojson.CloneObject(v))
		}
	}
	return out
}

// listFiles returns regular files under base/rel, relative to base.
func listFiles(base, rel string) ([]string, error) {
	root := filepath.Join(base, rel)
	if !dirExists(root) {
		return nil, nil
	}
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		r, err := filepath.Rel(base, path)
		if err != nil {
			return err
		}
		out = append(out, r)
		return nil
	})
	return out, err
}

// copyTree copies every regular file under src into dst.
func copyTree(src, dst string) error {
	files, err := listFiles(src, ".")
	if err != nil {
		return err
	}
	for _, rel := range files {
		if err := copyFile(filepath.Join(src, rel), filepath.Join(dst, rel)); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return writeFileAtomic(dst, data, 0o644)
}

// resolveWriteTarget follows symlinks so an atomic replace updates the
// link's target instead of replacing the link itself. A dangling link
// resolves to the path it points at.
func resolveWriteTarget(path string) (string, error) {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real, nil
	}
	cur := path
	for i := 0; i < 40; i++ {
		info, err := os.Lstat(cur)
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			return cur, nil
		}
		target, err := os.Readlink(cur)
		if err != nil {
			return "", err
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(cur), target)
		}
		cur = target
	}
	return "", fmt.Errorf("too many levels of symlinks: %s", path)
}

// writeFileAtomic writes data to a temp file next to the (symlink-resolved)
// target and renames it over the target, so readers never observe a partial
// file and symlinks such as a dotfiles-managed opencode.json are preserved.
// The target's mode is kept; the directory is fsynced after the rename.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	target, err := resolveWriteTarget(path)
	if err != nil {
		return err
	}
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if info, err := os.Stat(target); err == nil {
		perm = info.Mode().Perm()
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(target)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	fail := func(err error) error {
		tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return fail(err)
	}
	if err := tmp.Sync(); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, target); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		d.Close()
	}
	return nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// ReadStoreFiles returns the raw bytes of profiles.json and templates.json.
func ReadStoreFiles(p Paths) (profiles, templates []byte, err error) {
	profiles, err = os.ReadFile(p.ProfilesFile())
	if err != nil {
		return nil, nil, err
	}
	templates, err = os.ReadFile(p.TemplatesFile())
	if errors.Is(err, os.ErrNotExist) {
		return profiles, nil, nil
	}
	return profiles, templates, err
}

// ParseStorePayload validates raw profiles/templates JSON as a v2 store and
// returns it (bound to p). It never touches the disk.
func ParseStorePayload(p Paths, profilesRaw, templatesRaw []byte) (*Store, error) {
	var head struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(profilesRaw, &head); err != nil {
		return nil, fmt.Errorf("profiles: %w", err)
	}
	if head.Version > StoreVersion {
		return nil, fmt.Errorf("%w (schema v%d)", ErrNewerStore, head.Version)
	}
	if head.Version != StoreVersion {
		return nil, fmt.Errorf("profiles: unsupported schema v%d", head.Version)
	}
	var profiles Profiles
	if err := json.Unmarshal(profilesRaw, &profiles); err != nil {
		return nil, fmt.Errorf("profiles: %w", err)
	}
	seen := map[string]bool{}
	for i := range profiles.Profiles {
		name := profiles.Profiles[i].Name
		if err := ValidateProfileName(name); err != nil {
			return nil, err
		}
		if seen[name] {
			return nil, fmt.Errorf("duplicate profile name %q", name)
		}
		seen[name] = true
		if profiles.Profiles[i].Models == nil {
			profiles.Profiles[i].Models = map[string]Assignment{}
		}
	}
	profiles.Sort()
	sources := ojson.NewObject()
	if len(templatesRaw) > 0 && string(templatesRaw) != "null" {
		doc, err := ojson.ParseObject(templatesRaw)
		if err != nil {
			return nil, fmt.Errorf("templates: %w", err)
		}
		if s, ok := doc.GetObject("sources"); ok {
			sources = s
		}
	}
	return &Store{Paths: p, Profiles: profiles, Sources: sources}, nil
}

// UniqueSuffix returns a timestamp suffix with nanoseconds, unique enough
// for aside files (callers still check for collisions).
func UniqueSuffix() string {
	return Now().UTC().Format("20060102T150405.000000000Z")
}

// asidePath returns path + "." + label + "-" + unique suffix, never an
// existing file.
func asidePath(path, label string) string {
	base := path + "." + label + "-" + UniqueSuffix()
	cand := base
	for i := 1; ; i++ {
		if _, err := os.Lstat(cand); os.IsNotExist(err) {
			return cand
		}
		cand = fmt.Sprintf("%s-%d", base, i)
	}
}

// AsidePath is asidePath for other packages.
func AsidePath(path, label string) string { return asidePath(path, label) }

// moveStoreAside renames the store files to *.<label>-<unique>.
func moveStoreAside(p Paths, label string) ([]string, error) {
	var moved []string
	for _, f := range []string{p.ProfilesFile(), p.TemplatesFile()} {
		if _, err := os.Lstat(f); err != nil {
			continue
		}
		aside := asidePath(f, label)
		if err := os.Rename(f, aside); err != nil {
			return moved, err
		}
		moved = append(moved, aside)
	}
	return moved, nil
}

// RestoreProfilesFile atomically writes previously read profiles.json bytes
// back (used to roll back a failed rename).
func RestoreProfilesFile(p Paths, data []byte) error {
	return writeFileAtomic(p.ProfilesFile(), data, 0o644)
}
