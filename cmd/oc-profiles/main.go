// Command oc-profiles manages OpenCode ODD profiles: per-profile clones of
// gentle-ai's ODD orchestrator, explore/worker/verify subagents and optional
// Judgment Day judges, each with its own model and reasoning effort.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strings"

	"github.com/Teixeira49/oc-profiles/internal/backup"
	"github.com/Teixeira49/oc-profiles/internal/core"
	"github.com/Teixeira49/oc-profiles/internal/tui"
)

const usage = `oc-profiles - manage OpenCode ODD profiles

Usage:
  oc-profiles [flags]            open the TUI
  oc-profiles import [--force [--allow-empty]]
                                 build the store from the profiles in opencode.json
  oc-profiles migrate            convert a v1 (gentle-ai 3.7 SDD) store to ODD profiles
  oc-profiles apply [--dry-run]  re-apply the store to opencode.json
  oc-profiles rename OLD NEW [--no-apply]
                                 rename a profile (keeps its models), then apply
  oc-profiles list               list stored profiles
  oc-profiles version            print the version (also --version)
  oc-profiles sync               recover the store if needed, re-apply, write a backup snapshot
  oc-profiles backup list        list backup snapshots
  oc-profiles backup restore [--seq N] --yes
                                 restore the store from a snapshot (newest valid by default), then sync

Flags (any command):
  --opencode-dir DIR   OpenCode config dir   (env OC_PROFILES_OPENCODE_DIR, default ~/.config/opencode)
  --store-dir DIR      oc-profiles store dir (env OC_PROFILES_HOME, default ~/.config/oc-profiles)
  --variants FILE      effort variants cache (env OC_PROFILES_VARIANTS_FILE,
                       default ~/.gentle-ai/cache/model-variants.json)
  --backup-dir DIR     backup mirror dir     (env OC_PROFILES_BACKUP_DIR,
                       default ~/.local/share/oc-profiles/backup)
`

// version is overridden at build time with -ldflags "-X main.version=...".
// When unset (e.g. `go install ...@vX.Y.Z`), resolveVersion falls back to the
// module version recorded in the binary's build info.
var version = "dev"

func resolveVersion(stamped string, info *debug.BuildInfo, ok bool) string {
	if stamped != "dev" || !ok || info == nil {
		return stamped
	}
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return strings.TrimPrefix(v, "v")
	}
	return stamped
}

func main() {
	info, ok := debug.ReadBuildInfo()
	version = resolveVersion(version, info, ok)
	backup.Version = version
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "oc-profiles:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	cmd := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}

	paths, err := core.DefaultPaths()
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("oc-profiles", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, usage) }
	fs.StringVar(&paths.OpencodeDir, "opencode-dir", paths.OpencodeDir, "")
	fs.StringVar(&paths.StoreDir, "store-dir", paths.StoreDir, "")
	fs.StringVar(&paths.VariantsFile, "variants", paths.VariantsFile, "")
	fs.StringVar(&paths.BackupDir, "backup-dir", paths.BackupDir, "")
	yes := fs.Bool("yes", false, "")
	seq := fs.Int64("seq", 0, "")
	force := fs.Bool("force", false, "")
	allowEmpty := fs.Bool("allow-empty", false, "")
	dryRun := fs.Bool("dry-run", false, "")
	noApply := fs.Bool("no-apply", false, "")
	showVersion := fs.Bool("version", false, "")
	// Flags may appear before, between or after positional arguments.
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil
			}
			return err
		}
		if fs.NArg() == 0 {
			break
		}
		positional = append(positional, fs.Arg(0))
		args = fs.Args()[1:]
	}
	if *showVersion || cmd == "version" {
		fmt.Fprintf(stdout, "oc-profiles %s\n", version)
		return nil
	}
	wantArgs := map[string]int{"rename": 2, "backup": 1}[cmd]
	if len(positional) != wantArgs {
		switch cmd {
		case "rename":
			return fmt.Errorf("usage: oc-profiles rename OLD NEW [--no-apply]")
		case "backup":
			return fmt.Errorf("usage: oc-profiles backup list | backup restore [--seq N] --yes")
		}
		return fmt.Errorf("unexpected arguments: %v", positional)
	}

	// Read-only commands never mutate the store: they only diagnose. Mutating
	// commands auto-recover a missing/corrupt store from the backup mirror
	// first (import and restore manage the store themselves; the TUI recovers
	// on start).
	readOnly := cmd == "list" || cmd == "help" || (cmd == "apply" && *dryRun) || (cmd == "backup" && positional[0] == "list")
	switch {
	case readOnly:
		if err := diagnoseStore(paths, cmd == "backup", stderr); err != nil {
			return err
		}
	case cmd != "import" && cmd != "" && !(cmd == "backup" && positional[0] == "restore"):
		if err := recoverStore(paths, stdout, stderr); err != nil {
			return err
		}
	}

	switch cmd {
	case "":
		return tui.Run(paths)
	case "sync":
		return cmdSync(paths, stdout)
	case "backup":
		switch positional[0] {
		case "list":
			return cmdBackupList(paths, stdout)
		case "restore":
			return cmdBackupRestore(paths, *seq, *yes, stdout)
		}
		return fmt.Errorf("unknown backup command %q (use list or restore)", positional[0])
	case "import":
		return cmdImport(paths, core.ImportOptions{Force: *force, AllowEmpty: *allowEmpty}, stdout)
	case "migrate":
		return cmdMigrate(paths, stdout)
	case "apply":
		return cmdApply(paths, *dryRun, stdout)
	case "rename":
		return cmdRename(paths, positional[0], positional[1], *noApply, stdout)
	case "list":
		return cmdList(paths, stdout)
	case "help":
		fmt.Fprint(stdout, usage)
		return nil
	default:
		return fmt.Errorf("unknown command %q\n\n%s", cmd, usage)
	}
}

func cmdImport(paths core.Paths, opts core.ImportOptions, out io.Writer) error {
	_, res, err := core.Import(paths, opts)
	if err != nil {
		if errors.Is(err, core.ErrStoreExists) {
			return fmt.Errorf("%w: %s", err, paths.StoreDir)
		}
		return err
	}
	for _, m := range res.MovedAside {
		fmt.Fprintf(out, "Previous store file kept as %s\n", m)
	}
	for _, w := range res.Warnings {
		fmt.Fprintf(out, "  ! %s\n", w)
	}
	fmt.Fprintf(out, "Imported %d profiles into %s\n", len(res.Profiles), paths.StoreDir)
	for _, p := range res.Profiles {
		fmt.Fprintf(out, "  • %s\n", p)
	}
	if res.FromSDD {
		fmt.Fprintln(out, "Converted from gentle-ai 3.7 SDD profile agents; the next apply removes the SDD agents.")
	}
	for _, m := range res.MissingSources {
		fmt.Fprintf(out, "  ! gentle-ai agent not found (no template snapshot): %s\n", m)
	}
	return snapshot(paths, out)
}

func cmdMigrate(paths core.Paths, out io.Writer) error {
	res, err := core.Migrate(paths)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Migrated %d SDD profiles to ODD profiles. Old store backed up to %s\n", len(res.Profiles), res.Backup)
	for _, p := range res.Profiles {
		fmt.Fprintf(out, "  • %s\n", p.Name)
		for _, r := range core.Roles() {
			fmt.Fprintf(out, "      %-13s %s\n", r.Label, p.Get(r.ID).Label("(default)"))
		}
	}
	for _, w := range res.Warnings {
		fmt.Fprintf(out, "  ! %s\n", w)
	}
	fmt.Fprintln(out, "Run `oc-profiles apply` to generate the ODD agents and remove the retired SDD agents/files.")
	return snapshot(paths, out)
}

func loadStore(paths core.Paths) (*core.Store, error) {
	if !core.StoreExists(paths) {
		return nil, fmt.Errorf("no store at %s (run `oc-profiles import` first)", paths.StoreDir)
	}
	s, err := core.LoadStore(paths)
	if errors.Is(err, core.ErrLegacyStore) {
		return nil, fmt.Errorf("%w (%s)", err, paths.StoreDir)
	}
	return s, err
}

func cmdRename(paths core.Paths, oldName, newName string, noApply bool, out io.Writer) error {
	s, err := loadStore(paths)
	if err != nil {
		return err
	}
	if err := s.Profiles.Rename(oldName, newName); err != nil {
		if errors.Is(err, core.ErrRenameNoop) {
			fmt.Fprintln(out, "Name unchanged; nothing to do.")
			return nil
		}
		return err
	}
	previous, _, err := core.ReadStoreFiles(paths)
	if err != nil {
		return err
	}
	if err := s.SaveProfiles(); err != nil {
		return err
	}
	fmt.Fprintf(out, "Renamed profile %q to %q in %s\n", oldName, newName, paths.ProfilesFile())
	if noApply {
		fmt.Fprintln(out, "Skipped apply (--no-apply); run `oc-profiles apply` to update opencode.json.")
		return snapshot(paths, out)
	}
	// A rename always changes agent keys, so apply writes opencode.json and
	// the backup snapshot itself.
	if err := cmdApply(paths, false, out); err != nil {
		// Keep the store and opencode.json consistent: undo the rename.
		if rbErr := core.RestoreProfilesFile(paths, previous); rbErr != nil {
			return fmt.Errorf("apply failed (%v) and rolling back the rename also failed: %w", err, rbErr)
		}
		return fmt.Errorf("apply failed, rename rolled back (profile is still %q): %w", oldName, err)
	}
	return nil
}

func cmdApply(paths core.Paths, dryRun bool, out io.Writer) error {
	s, err := loadStore(paths)
	if err != nil {
		return err
	}
	var res core.ApplyResult
	if dryRun {
		res, err = s.Plan()
	} else {
		res, err = s.Apply()
	}
	if err != nil {
		return err
	}
	verb := "Applied"
	if dryRun {
		verb = "Would apply"
	}
	for _, w := range res.Warnings {
		fmt.Fprintf(out, "  ! %s\n", w)
	}
	if res.InSync() {
		fmt.Fprintln(out, "opencode.json is already in sync; nothing to do.")
		return nil
	}
	printApply(out, verb, paths, res)
	if dryRun {
		return nil
	}
	return snapshot(paths, out)
}

func printApply(out io.Writer, verb string, paths core.Paths, res core.ApplyResult) {
	// Agents added without any removal means they had gone missing (for
	// example after a gentle-ai sync); with removals it is a rename/restore.
	if n := res.MissingCount(); n > 0 && len(res.Removed) == 0 {
		fmt.Fprintf(out, "Restored %d missing profile agents.\n", n)
	}
	fmt.Fprintf(out, "%s to %s\n", verb, paths.OpencodeJSON())
	printList(out, "added", res.Added)
	printList(out, "updated", res.Updated)
	printList(out, "removed", res.Removed)
	printList(out, "removed file", res.RemovedFiles)
	if res.Backup != "" {
		fmt.Fprintf(out, "opencode.json backup: %s\n", res.Backup)
	}
}

// diagnoseStore reports a missing/corrupt store for read-only commands
// without changing anything. allowMissing lets `backup list` proceed.
func diagnoseStore(paths core.Paths, allowMissing bool, errOut io.Writer) error {
	pr := backup.Diagnose(paths)
	if !pr.Missing && pr.LoadErr == nil {
		return nil
	}
	hint := ""
	if pr.CanFix {
		hint = fmt.Sprintf("; run `oc-profiles sync` to restore it from backup snapshot #%d (%s)", pr.Snapshot.Seq, pr.Snapshot.CreatedAt)
	}
	problem := "store is missing"
	if pr.LoadErr != nil {
		problem = "store is corrupt: " + pr.LoadErr.Error()
	}
	if allowMissing {
		fmt.Fprintf(errOut, "oc-profiles: warning: %s%s\n", problem, hint)
		return nil
	}
	if pr.Missing && !pr.CanFix {
		return nil // first run: the command reports the missing store itself
	}
	return fmt.Errorf("%s%s", problem, hint)
}

// recoverStore restores a missing/corrupt store from the mirror and prints
// what happened.
func recoverStore(paths core.Paths, out, errOut io.Writer) error {
	rep, err := backup.Recover(paths)
	for _, w := range rep.Warnings {
		fmt.Fprintf(errOut, "oc-profiles: warning: %s\n", w)
	}
	if err != nil {
		return err
	}
	if rep.Restored {
		fmt.Fprintf(out, "Recovered the store from backup snapshot #%d (%s, %d profiles); reason: %s\n",
			rep.From.Seq, rep.From.CreatedAt, rep.From.Profiles, rep.Reason)
		for _, m := range rep.MovedAside {
			fmt.Fprintf(out, "  previous file kept as %s\n", m)
		}
	}
	return nil
}

// snapshot writes the backup mirror; a failure is reported loudly and makes
// the command exit non-zero, but never touches the store.
func snapshot(paths core.Paths, out io.Writer) error {
	res, err := backup.Snapshot(paths)
	if err != nil {
		return fmt.Errorf("BACKUP FAILED (the store and opencode.json are fine): %w", err)
	}
	printSnapshot(out, res)
	return nil
}

func printSnapshot(out io.Writer, res backup.SnapshotResult) {
	state := "unchanged"
	if res.Written {
		state = "new"
	}
	fmt.Fprintf(out, "Backup snapshot #%d (%s, %s)\n", res.Info.Seq, res.Info.CreatedAt, state)
}

func cmdSync(paths core.Paths, out io.Writer) error {
	res, err := backup.Sync(paths)
	if err != nil {
		return err
	}
	for _, w := range res.Apply.Warnings {
		fmt.Fprintf(out, "  ! %s\n", w)
	}
	if res.Apply.InSync() {
		fmt.Fprintln(out, "opencode.json is already in sync.")
	} else {
		printApply(out, "Synced", paths, res.Apply)
	}
	if res.BackupErr != nil {
		return fmt.Errorf("BACKUP FAILED (the store and opencode.json are fine): %w", res.BackupErr)
	}
	printSnapshot(out, res.Snapshot)
	return nil
}

func cmdBackupList(paths core.Paths, out io.Writer) error {
	list := backup.List(paths.BackupDir)
	if len(list) == 0 {
		fmt.Fprintf(out, "No snapshots in %s\n", paths.BackupDir)
		return nil
	}
	fmt.Fprintf(out, "Snapshots in %s (store last mirrored #%d):\n", paths.BackupDir, backup.LastSeq(paths))
	for _, info := range list {
		status := "valid"
		if !info.Valid {
			status = "CORRUPT: " + info.Err.Error()
		}
		cur := ""
		if info.Current {
			cur = " (current)"
		}
		fmt.Fprintf(out, "  #%-4d %-20s %2d profiles  %s%s\n", info.Seq, info.CreatedAt, info.Profiles, status, cur)
	}
	return nil
}

func cmdBackupRestore(paths core.Paths, seq int64, yes bool, out io.Writer) error {
	if !yes {
		return fmt.Errorf("restore replaces the store; re-run with --yes to confirm")
	}
	info, warnings, err := backup.RestoreSeq(paths, seq)
	for _, w := range warnings {
		fmt.Fprintf(out, "  ! %s\n", w)
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Restored the store from snapshot #%d (%s, %d profiles); previous files kept as *.replaced-*\n", info.Seq, info.CreatedAt, info.Profiles)
	return cmdSync(paths, out)
}

func printList(out io.Writer, label string, items []string) {
	for _, it := range items {
		fmt.Fprintf(out, "  %s: %s\n", label, it)
	}
}

func cmdList(paths core.Paths, out io.Writer) error {
	s, err := loadStore(paths)
	if err != nil {
		return err
	}
	for _, p := range s.Profiles.Profiles {
		fmt.Fprintf(out, "%s\t%s\n", p.Name, p.Get(core.RoleOrchestrator).Label("(default)"))
	}
	return nil
}
