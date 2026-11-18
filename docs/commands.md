# Command reference

```
oc-profiles [command] [arguments] [flags]
```

With no command, oc-profiles opens the TUI (see [usage.md](usage.md)). Flags
are parsed by every command and may appear before, between or after the
arguments; a command ignores flags it doesn't use.

The example outputs below come from real runs against temporary directories
(paths shortened to `/tmp/demo`).

## Global flags and environment

| Flag | Environment variable | Default | Purpose |
| --- | --- | --- | --- |
| `--opencode-dir DIR` | `OC_PROFILES_OPENCODE_DIR` | `~/.config/opencode` | Directory holding `opencode.json` |
| `--store-dir DIR` | `OC_PROFILES_HOME` | `~/.config/oc-profiles` | The store (`profiles.json`, `templates.json`) |
| `--backup-dir DIR` | `OC_PROFILES_BACKUP_DIR` | `~/.local/share/oc-profiles/backup` | Backup mirror (must be absolute) |
| `--variants FILE` | `OC_PROFILES_VARIANTS_FILE` | `~/.gentle-ai/cache/model-variants.json` | gentle-ai's reasoning-effort cache (TUI picker) |
| | `OC_PROFILES_OPENCODE_BIN` | `opencode` from `PATH` | Binary used for `opencode models` (TUI picker) |
| `--version` | | | Print the version and exit |
| `-h`, `--help` | | | Print usage to stderr and exit 0 |

A flag beats its environment variable, which beats the default.

Command-specific flags: `--force`, `--allow-empty` (import), `--dry-run`
(apply), `--no-apply` (rename), `--seq N`, `--yes` (backup restore).

## Exit codes

| Code | Meaning |
| --- | --- |
| `0` | Success, including "already in sync" and "Name unchanged; nothing to do." |
| `1` | Any error. The message is printed to stderr as `oc-profiles: <message>`. This includes `BACKUP FAILED (the store and opencode.json are fine): ...`, where the main work succeeded but the backup mirror couldn't be written. |

## Store recovery before commands

- **Mutating commands** (`migrate`, `apply`, `sync`, `rename`, `backup restore`
  via its sync) first restore a missing or corrupt store from the newest valid
  backup snapshot and print `Recovered the store from backup snapshot #N (...); reason: ...`.
- **Read-only commands** (`list`, `apply --dry-run`, `backup list`) never
  change anything. With a broken store they report
  `store is missing; run \`oc-profiles sync\` to restore it from backup snapshot #N (...)`
  (or `store is corrupt: ...`). `backup list` prints it as a warning and still
  lists the snapshots.
- `import` and `backup restore` manage the store themselves and skip this step.

See [architecture.md](architecture.md#backup-mirror-and-recovery) for the
precedence rules.

## `oc-profiles` (TUI)

Opens the interactive menu. Recovers the store and regenerates missing
profile agents on start.

## `import`

```
oc-profiles import [--force [--allow-empty]]
```

Builds the store from `opencode.json`: one profile per
`odd-orchestrator-<name>` agent (reserved or invalid names are skipped with a
warning). With no ODD profiles it converts gentle-ai 3.7
`sdd-orchestrator-<name>` profiles and schedules the SDD cleanup. It also
snapshots gentle-ai's source agents into `templates.json`, then writes a
backup snapshot.

| Flag | Effect |
| --- | --- |
| `--force` | Replace an existing store. The old files are kept as `profiles.json.replaced-<time>` and `templates.json.replaced-<time>`. |
| `--allow-empty` | With `--force`, allow replacing a store that has profiles with one that has none. Without it, that case fails with `import found no profiles but the existing store has some; pass --allow-empty to replace it anyway`. |

```
$ oc-profiles import
Imported 1 profiles into /tmp/demo/store
  • work
Backup snapshot #1 (2026-11-18T17:00:50Z, new)

$ oc-profiles import
oc-profiles: store already exists (use --force to overwrite): /tmp/demo/store

$ oc-profiles import --force
Previous store file kept as /tmp/demo/store/profiles.json.replaced-20261118T170050.659701000Z
Previous store file kept as /tmp/demo/store/templates.json.replaced-20261118T170050.659804000Z
Imported 1 profiles into /tmp/demo/store
  • work-2
Backup snapshot #4 (2026-11-18T17:00:50Z, unchanged)
```

## `migrate`

```
oc-profiles migrate
```

Converts a v1 store (gentle-ai 3.7 SDD profiles) to the v2 ODD schema. The
whole old store is first copied to `<store>.bak-v1-<time>`. Mapping:
orchestrator ← `sdd-orchestrator-P`, explore ← `sdd-explore-P`, worker ←
`sdd-apply-P`, verify ← `sdd-verify-P`, judges ← `jd-judge-a/b-P`; the fix
agent stays unassigned. The next `apply` performs the one-time SDD cleanup.
Safe to re-run if interrupted.

```
$ oc-profiles migrate
oc-profiles: store is already schema v2; nothing to migrate
```

## `apply`

```
oc-profiles apply [--dry-run]
```

Reconciles `opencode.json` with the store, using gentle-ai's live agents as
templates (see [the apply pipeline](architecture.md#the-apply-pipeline)).
Writes `opencode.json.bak-oc-profiles-<time>` before changing the file (the
newest 10 are kept) and a backup snapshot when anything changed. Nothing is
written when the result is identical.

| Flag | Effect |
| --- | --- |
| `--dry-run` | Show what would change; write nothing (read-only). |

```
$ oc-profiles apply --dry-run
Restored 3 missing profile agents.
Would apply to /tmp/demo/oc/opencode.json
  added: odd-explore-work
  added: odd-worker-work
  added: odd-verify-work
  updated: odd-orchestrator-work

$ oc-profiles apply
Restored 3 missing profile agents.
Applied to /tmp/demo/oc/opencode.json
  added: odd-explore-work
  added: odd-worker-work
  added: odd-verify-work
  updated: odd-orchestrator-work
opencode.json backup: /tmp/demo/oc/opencode.json.bak-oc-profiles-20261118130050
Backup snapshot #1 (2026-11-18T17:00:50Z, unchanged)

$ oc-profiles apply
opencode.json is already in sync; nothing to do.
```

Warnings are printed as `  ! <message>` lines. Errors that stop apply without
writing anything: `refusing to overwrite agents not managed by oc-profiles: ...`,
`validation failed, nothing written: ...`, `opencode.json changed during apply; re-run`.

## `sync`

```
oc-profiles sync
```

Recovers the store if needed, applies it (regenerating any missing profile
agents), then writes a backup snapshot. Run it after `gentle-ai sync` or
"Configure models".

```
$ oc-profiles sync
opencode.json is already in sync.
Backup snapshot #1 (2026-11-18T17:00:50Z, unchanged)
```

## `rename`

```
oc-profiles rename OLD NEW [--no-apply]
```

Renames a profile and keeps every model assignment, judges and fix agent
included. NEW must be a valid, unused slug. Then applies: the `-OLD` agents are
removed and the `-NEW` ones generated. If that apply fails, the rename is
rolled back (`apply failed, rename rolled back (profile is still "OLD"): ...`).

| Flag | Effect |
| --- | --- |
| `--no-apply` | Only rename in the store (and snapshot it); run `apply` later. |

```
$ oc-profiles rename work work-2
Renamed profile "work" to "work-2" in /tmp/demo/store/profiles.json
Applied to /tmp/demo/oc/opencode.json
  added: odd-orchestrator-work-2
  added: odd-explore-work-2
  added: odd-worker-work-2
  added: odd-verify-work-2
  removed: odd-explore-work
  removed: odd-orchestrator-work
  removed: odd-verify-work
  removed: odd-worker-work
opencode.json backup: /tmp/demo/oc/opencode.json.bak-oc-profiles-20261118130050-1
Backup snapshot #2 (2026-11-18T17:00:50Z, new)

$ oc-profiles rename work-2 Bad_Name
oc-profiles: profile name "Bad_Name" must be lowercase alphanumeric with hyphens (no leading/trailing hyphen)

$ oc-profiles rename work-2 work --no-apply
Renamed profile "work-2" to "work" in /tmp/demo/store/profiles.json
Skipped apply (--no-apply); run `oc-profiles apply` to update opencode.json.
Backup snapshot #3 (2026-11-18T17:00:50Z, new)
```

## `list`

```
oc-profiles list
```

Prints one `name<TAB>orchestrator model [variant]` line per profile. Read-only.

```
$ oc-profiles list
work	anthropic/claude-opus-5-5 [high]
```

## `backup list`

```
oc-profiles backup list
```

Lists snapshots newest first: sequence, UTC time, profile count, `valid` or
`CORRUPT: <reason>`, and `(current)`. Read-only.

```
$ oc-profiles backup list
Snapshots in /tmp/demo/backup (store last mirrored #3):
  #3    2026-11-18T17:00:50Z  1 profiles  valid (current)
  #2    2026-11-18T17:00:50Z  1 profiles  valid
  #1    2026-11-18T17:00:50Z  1 profiles  valid
```

## `backup restore`

```
oc-profiles backup restore [--seq N] --yes
```

Replaces the store with snapshot N (default: the newest snapshot whose
checksum verifies and whose content is a valid store), keeping the previous
files as `*.replaced-<time>`, then runs `sync`.

| Flag | Effect |
| --- | --- |
| `--seq N` | Snapshot to restore. An invalid explicit snapshot is an error. |
| `--yes` | Required confirmation. |

```
$ oc-profiles backup restore --seq 1
oc-profiles: restore replaces the store; re-run with --yes to confirm

$ oc-profiles backup restore --seq 2 --yes
Restored the store from snapshot #2 (2026-11-18T17:00:50Z, 1 profiles); previous files kept as *.replaced-*
Synced to /tmp/demo/oc/opencode.json
  added: odd-orchestrator-work-2
  ...
Backup snapshot #4 (2026-11-18T17:00:50Z, new)
```

## `version` / `--version`

```
$ oc-profiles --version
oc-profiles 0.4.0
```

## `help`

Prints the usage summary to stdout (`-h`/`--help` print it to stderr).
