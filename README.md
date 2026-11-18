# oc-profiles

A small CLI/TUI to create, edit and delete **OpenCode ODD profiles**. A profile
`P` is a set of clones of gentle-ai's ODD agents, each with its own model and
reasoning effort:

| Role | Generated agent | Cloned from (live) |
| --- | --- | --- |
| Orchestrator (primary) | `odd-orchestrator-P` | `gentle-orchestrator` |
| Explore | `odd-explore-P` | `gentle-ai-explore` |
| Worker | `odd-worker-P` | `gentle-ai-worker` |
| Verify | `odd-verify-P` | `gentle-ai-verify` |
| Judge A / B (optional) | `jd-judge-a-P` / `jd-judge-b-P` | `jd-judge-a` / `jd-judge-b` |
| Fix Agent (optional) | `jd-fix-agent-P` | `jd-fix-agent` |

In the profile orchestrator's prompt and `permission.task`, references to the
gentle-ai agents are rewritten (whole tokens only) to the profile agents.
Tokens in a path or file-reference context (`prompts/gentle-ai-worker.md`,
`skills/gentle-ai-verify/SKILL.md`, `{file:...}`, `name:`) are left as is.
Optional roles left unassigned keep pointing at the global gentle-ai agent.
`review-*` agents are shared, never cloned. Every generated description ends
with `(P profile) [oc-profiles]`, so `gentle-ai sync` treats them as
user-owned and leaves them alone.

## Why it exists

gentle-ai removed its profiles menu in v4.0.0. oc-profiles owns the profile
model assignments in `~/.config/oc-profiles/` and regenerates the agents from
the **live** gentle-ai agents on every apply, so upstream improvements to the
ODD prompts propagate to every profile. The menu mirrors gentle-ai 3.7 (same
colors, `j/k`, `enter`, `n`, `d`, `esc`), and adds editing.

## Install

```sh
make install                         # -> ~/.local/bin/oc-profiles
make install PREFIX=/opt/homebrew/bin
make uninstall                       # removes the binary only
```

## Usage

```sh
oc-profiles import     # first run: build the store from opencode.json
                       # --force replaces an existing store (old files moved aside);
                       # add --allow-empty to replace it with zero profiles
oc-profiles migrate    # convert a v1 store (gentle-ai 3.7 SDD profiles) to ODD profiles
oc-profiles            # TUI: list / create (n) / edit (enter) / rename (r) / delete (d) / apply (a)
oc-profiles list       # print profiles and their orchestrator model
oc-profiles apply      # re-apply the store to opencode.json (--dry-run to preview)
oc-profiles rename OLD NEW [--no-apply]   # rename a profile, keeping all its models
oc-profiles sync       # recover the store if needed, re-apply, write a backup snapshot
oc-profiles backup list                   # list backup snapshots (valid/corrupt)
oc-profiles backup restore [--seq N] --yes  # restore the store from a snapshot, then sync
```

Renaming keeps every model/variant assignment (including optional judges and
the fix agent): apply removes the `odd-*-OLD`, `jd-judge-{a,b}-OLD` and
`jd-fix-agent-OLD` agents and generates the `-NEW` ones. The new name follows
the same slug rules as create and must not belong to another profile.
Sessions bound to the old orchestrator name must switch to the new one.

**After `gentle-ai sync` (or "Configure models" in gentle-ai), run
`oc-profiles sync`.** Opening the TUI also regenerates any missing profile
agents automatically and shows what was restored. TUI keys: `s` sync,
`a` apply, `b` backups (restore asks for confirmation). Every TUI save applies
immediately and writes a backup snapshot.

## Backup mirror and auto-recovery

A mirror independent of gentle-ai and of the store lives in
`~/.local/share/oc-profiles/backup/` (`--backup-dir` /
`OC_PROFILES_BACKUP_DIR`):

- `current.json`: one self-contained snapshot (profiles + templates) with
  schema version, a monotonically increasing sequence number, a UTC timestamp,
  the oc-profiles version and a sha256 of the payload.
- `history/snap-<seq>.json`: the last 10 snapshots, rotated.

A snapshot is written atomically (temp file, fsync, rename) after every
successful store save, rename, migrate, import, sync, and every apply that
changes something. It is skipped when the store is unchanged. A failed backup
write never touches the store; the command reports `BACKUP FAILED` and exits
non-zero. Every read verifies the checksum, and corrupt snapshots are skipped
with a warning.

Recovery rule, applied at the start of every mutating command and of the
TUI. Read-only commands (`list`, `apply --dry-run`, `backup list`) never
change anything: they report the problem and tell you to run
`oc-profiles sync`.

1. **Store missing or unreadable** (not a legacy v1 store): restore it from
   the snapshot with the highest sequence (across `current.json` and history)
   whose checksum verifies and whose payload is a valid store, and print what
   was recovered. Both new files are staged first and then swapped in; the
   previous files are kept as `*.corrupt-<time>` (nanosecond suffix). With no
   valid snapshot, the command fails and leaves the files untouched.
2. **Store written by a newer oc-profiles**: refuse, and move nothing.
3. **Store valid**: the store wins. The store records the last sequence it
   was mirrored as (`backup-state.json`). If the newest snapshot has a higher
   sequence and different content, a warning names it and both are left as
   they are. The next save or sync writes the store as a newer snapshot, the
   other one stays in history, and you can opt in with
   `oc-profiles backup restore --seq N --yes`.

`backup restore` keeps the replaced store files as `*.replaced-<time>`.

## What `apply` does

1. Reads the live gentle-ai source agents from `opencode.json` (falling back,
   with a warning, to the store's last snapshot when one is missing).
2. Upserts every profile agent and removes profile agents (`odd-*-X`,
   `jd-judge-{a,b}-X`, `jd-fix-agent-X`) that the store no longer generates.
3. Validates: every allowed `permission.task` target of a profile agent exists,
   and no profile orchestrator prompt still names a gentle-ai ODD agent.
   On failure nothing is written.
4. Writes `opencode.json.bak-oc-profiles-<timestamp>` and replaces
   `opencode.json` atomically. Nothing is written if the result is
   semantically identical: differences in formatting alone (gentle-ai writes
   `<`, `>`, `&` as `\u003c`-style escapes) are not changes, and when it does
   write, oc-profiles keeps the file's escaping style.

gentle-ai's own agents and all other top-level keys are left untouched.
Before writing, apply re-reads `opencode.json`; if another program changed
it meanwhile, apply aborts ("opencode.json changed during apply; re-run").
The newest 10 `opencode.json.bak-oc-profiles-*` backups are kept.

### Ownership marker

Apply only overwrites or deletes agents whose description contains
`[oc-profiles]`. An agent without the marker at a key apply would generate
(for example your own `odd-worker-work`) makes that apply fail with a message
naming the key; one at a key apply would delete is kept, with a warning.
Rename the agent or the profile to resolve a conflict.

### Profile agents are generated

Profile agents are regenerated from the live gentle-ai agents and the store on
every apply. Manual edits to `odd-*-P`, `jd-judge-*-P` or `jd-fix-agent-P` in
`opencode.json` are overwritten; change models through oc-profiles instead.

### Symlinks

If `opencode.json`, `profiles.json` or `templates.json` is a symlink (e.g.
managed by stow or chezmoi), oc-profiles writes through it: the link stays in
place and its target is replaced atomically, keeping the target's mode.

## Migrating from SDD profiles

`oc-profiles migrate` backs up the v1 store to `<store>.bak-v1-<timestamp>` and
maps each SDD profile: orchestrator ← `sdd-orchestrator-P`, explore ←
`sdd-explore-P`, worker ← `sdd-apply-P`, verify ← `sdd-verify-P`, judges ←
`jd-judge-{a,b}-P`; the fix agent is left unassigned. The next `apply` also
removes, once, the `sdd-*` agents and the `explore`/`general` entries restored
from gentle-ai 3.7 (OpenCode has built-ins), but only those that carry
gentle-ai's `gentle-ai/sdd` marker or equal the v1 snapshot (with the profile
name substituted). Customized or unrelated agents are kept with a warning.
It also removes the SDD prompt/skill files whose content still matches the v1
snapshot (modified files are kept with a warning). Migration is safe to re-run
if it was interrupted.

## Store layout (`~/.config/oc-profiles/`)

| Path | Content |
| --- | --- |
| `profiles.json` | schema v2: each profile's per-role `model`/`variant` |
| `templates.json` | last good snapshot of the live gentle-ai source agents |
| `legacy/` | v1 snapshot kept only until the post-migration cleanup runs |

## Paths and data sources

| Flag | Env | Default |
| --- | --- | --- |
| `--opencode-dir` | `OC_PROFILES_OPENCODE_DIR` | `~/.config/opencode` |
| `--store-dir` | `OC_PROFILES_HOME` | `~/.config/oc-profiles` |
| `--variants` | `OC_PROFILES_VARIANTS_FILE` | `~/.gentle-ai/cache/model-variants.json` |
| `--backup-dir` | `OC_PROFILES_BACKUP_DIR` | `~/.local/share/oc-profiles/backup` |
| | `OC_PROFILES_OPENCODE_BIN` | `opencode` |

Models come from `opencode models`, where `opencode` is resolved from `PATH`
unless `OC_PROFILES_OPENCODE_BIN` names a binary. Reasoning efforts come from gentle-ai's
variants cache (the effort step is skipped when a model has none).

## Development

```sh
make vet test build
```

## License

Portions adapted from [gentle-ai](https://github.com/Gentleman-Programming/gentle-ai)
v3.7.0 (MIT). See `NOTICE`.
