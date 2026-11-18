package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/teixeira49/oc-profiles/internal/ojson"
)

// MigrateResult summarizes an SDD -> ODD store migration.
type MigrateResult struct {
	Backup   string
	Profiles []Profile
	Warnings []string
}

type v1Profile struct {
	Name         string                `json:"name"`
	Orchestrator Assignment            `json:"orchestrator"`
	Phases       map[string]Assignment `json:"phases"`
}

type v1Profiles struct {
	Profiles []v1Profile `json:"profiles"`
}

// Migrate converts a v1 (gentle-ai 3.7 SDD) store into a v2 ODD store.
//
// The whole old store is copied to "<store>.bak-v1-<ts>" first. The v1 asset
// snapshot and templates are kept under legacy/ so the next apply can remove
// the retired SDD agents and asset files (only files whose content still
// matches the snapshot).
func Migrate(p Paths) (MigrateResult, error) {
	var res MigrateResult
	v, err := StoreVersionOf(p)
	if err != nil {
		return res, err
	}
	if v >= StoreVersion {
		return res, fmt.Errorf("store is already schema v%d; nothing to migrate", v)
	}
	data, err := os.ReadFile(p.ProfilesFile())
	if err != nil {
		return res, err
	}
	var old v1Profiles
	if err := json.Unmarshal(data, &old); err != nil {
		return res, fmt.Errorf("parse v1 %s: %w", p.ProfilesFile(), err)
	}

	backup := asidePath(filepath.Clean(p.StoreDir), "bak-v1")
	if err := copyTree(p.StoreDir, backup); err != nil {
		return res, fmt.Errorf("backup store: %w", err)
	}
	res.Backup = backup

	profiles := Profiles{Version: StoreVersion, SDDCleanup: true}
	for _, op := range old.Profiles {
		if err := ValidateProfileName(op.Name); err != nil {
			return res, err
		}
		profiles.Profiles = append(profiles.Profiles, SDDToODD(op.Name, op.Orchestrator, op.Phases))
	}
	profiles.Sort()
	res.Profiles = profiles.Profiles

	// Keep the v1 artifacts needed for the one-time SDD cleanup. Every step is
	// copy-first and safe to repeat if a previous migrate was interrupted.
	if err := os.MkdirAll(p.LegacyDir(), 0o755); err != nil {
		return res, err
	}
	oldAssets := filepath.Join(p.StoreDir, "assets")
	switch {
	case dirExists(p.legacyAssetsDir()):
		// Resumed migration: keep the snapshot copied last time.
	case dirExists(oldAssets):
		if err := copyTree(oldAssets, p.legacyAssetsDir()); err != nil {
			return res, fmt.Errorf("copy v1 assets: %w", err)
		}
	default:
		res.Warnings = append(res.Warnings, "v1 store has no asset snapshot; SDD prompt/skill files will be left in place")
	}
	if !fileExists(p.legacyTemplatesFile()) && isV1Templates(p.TemplatesFile()) {
		if err := copyFile(p.TemplatesFile(), p.legacyTemplatesFile()); err != nil {
			return res, err
		}
	}

	sources := snapshotSourcesFromDisk(p, &res.Warnings)
	s := &Store{Paths: p, Profiles: profiles, Sources: sources}
	if err := s.Save(); err != nil {
		return res, err
	}
	// Only now that the v2 store is saved, drop the v1 asset copy.
	if dirExists(oldAssets) {
		if err := os.RemoveAll(oldAssets); err != nil {
			res.Warnings = append(res.Warnings, "could not remove the v1 assets directory: "+err.Error())
		}
	}
	return res, nil
}

// isV1Templates reports whether path is a v1 templates.json.
func isV1Templates(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var head struct {
		Version       int             `json:"version"`
		ProfileAgents json.RawMessage `json:"profileAgents"`
	}
	if json.Unmarshal(data, &head) != nil {
		return false
	}
	return head.Version <= 1 && len(head.ProfileAgents) > 0
}

func snapshotSourcesFromDisk(p Paths, warnings *[]string) *ojson.Object {
	_, agents, _, err := ReadOpencode(p)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			*warnings = append(*warnings, "could not read opencode.json for the template snapshot: "+err.Error())
		}
		return ojson.NewObject()
	}
	src := snapshotSources(agents)
	for _, name := range SourceNames() {
		if !src.Has(name) {
			*warnings = append(*warnings, "gentle-ai agent "+name+" not found in opencode.json")
		}
	}
	return src
}
