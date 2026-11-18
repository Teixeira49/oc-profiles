# Usage

- [The TUI](#the-tui)
- [Workflows](#workflows)
- [Troubleshooting](#troubleshooting)

## The TUI

Run `oc-profiles` with no arguments. `ctrl+c` quits from any screen.

On start, the TUI:

1. Restores a missing or corrupt store from the backup mirror, if a valid
   snapshot exists ("Recovered the store from backup snapshot #N ...").
2. Opens the **Import** screen if there is no store, or the **Migrate** screen
   if the store holds gentle-ai 3.7 SDD profiles.
3. Regenerates profile agents that are missing from `opencode.json` (for
   example after `gentle-ai sync`) and shows "Restored N missing profile
   agents: ...". This step is skipped while a post-migration SDD cleanup is
   pending; that cleanup only runs when you press `a` or `s`.

### Profile list ("ODD Profiles")

Lists every profile with its orchestrator model, then "Create new profile"
and "Quit".

| Key | Action |
| --- | --- |
| `j` / `k` (or arrows) | Move |
| `enter` | Edit the selected profile; on "Create new profile" create one; on "Quit" quit |
| `e` | Edit the selected profile |
| `n` | Create a new profile |
| `r` | Rename the selected profile |
| `d` | Delete the selected profile (asks for confirmation) |
| `a` | Apply the store to `opencode.json` |
| `s` | Sync: recover the store if needed, apply, write a backup snapshot |
| `b` | Open the Backups screen |
| `esc` / `q` | Quit |

A banner above the list tells you when `opencode.json` needs an apply:

| Banner | Meaning |
| --- | --- |
| `⚠ N agents are missing from opencode.json (gentle-ai sync may have removed them). Press a to apply.` | Profile agents are missing |
| `⚠ opencode.json is out of sync with the store. Press a to apply.` | Agents differ from what the store generates (for example gentle-ai updated its ODD prompts) |
| `⚠ Retired SDD agents and files are pending removal. Press a to apply.` | `migrate` ran; the one-time SDD cleanup hasn't been applied yet |

### Create and edit

1. **Name.** Type a slug (lowercase letters, digits and hyphens, no leading or
   trailing hyphen). Keys: `backspace`, `delete`, `left`/`right`,
   `home`/`ctrl+a`, `end`/`ctrl+e`; `enter` continues, `esc` goes back.
   Names already in use and reserved names (`default`, `orchestrator`,
   `jd-judge-a`, `jd-judge-b`, `jd-fix-agent`) are rejected. When editing,
   the name is read-only (use rename).
2. **Assign Models.** One row per role:

   | Row | Sets |
   | --- | --- |
   | Orchestrator (coordinator) | `odd-orchestrator-P` |
   | Set all subagents | Explore, Worker and Verify at once |
   | Explore / Worker / Verify | `odd-explore-P`, `odd-worker-P`, `odd-verify-P` |
   | Judge A / Judge B / Fix Agent | `jd-judge-a-P`, `jd-judge-b-P`, `jd-fix-agent-P` (optional) |

   `j`/`k` move (the "--- Judgment Day ---" separator is skipped), `enter`
   changes the model of the selected row, `backspace` clears it, `esc` goes
   back to the name step. A cleared core role shows `(default)` (OpenCode's
   default model); a cleared optional role shows `(global)`: the profile's
   orchestrator then delegates to gentle-ai's global `jd-judge-a`,
   `jd-judge-b` or `jd-fix-agent`. Choose "Continue" to go on.
3. **Choosing a model.**
   - Provider list: `j`/`k`, `enter`, `esc`.
   - Model list (newest first): type to search, `backspace` deletes a
     character, `ctrl+u` clears the search, `enter` selects, `esc` returns to
     the providers. Models with cached reasoning efforts show `(effort)`.
   - Effort list: shown only when gentle-ai's variants cache knows the model.
     `default` means no variant.
4. **Profile Summary.** "Create & Sync" (or "Save & Sync") saves the store,
   applies it and writes a backup snapshot; "Cancel" discards; `esc` goes
   back to the models.

### Rename

`r` opens an input prefilled with the current name. The same slug and
uniqueness rules apply; the same name is a no-op ("Name unchanged."). The
confirmation lists every `old-key → new-key`; "Rename & Sync" applies it. If
the apply fails, the rename is rolled back.

### Delete

`d` lists the agent keys that will be removed and defaults to "Cancel".
"Delete & Sync" removes the profile from the store and its agents from
`opencode.json`. A backup of `opencode.json` is written first.

### Backups

`b` lists the snapshots in the backup mirror (sequence, time, profile count,
`CORRUPT` for invalid ones, `(current)`). `enter` on a valid snapshot asks
"Replace the store with snapshot #N ...?"; "Restore & Sync" replaces the store
(the old files are kept as `*.replaced-<time>`) and syncs. `esc`/`q` go back.

## Workflows

### First run

```sh
oc-profiles import   # or open the TUI and choose "Import from opencode.json"
```

Import reads the existing `odd-orchestrator-<name>` profiles from
`opencode.json`. With none, it converts gentle-ai 3.7 `sdd-orchestrator-<name>`
profiles if present, otherwise it creates an empty store.

### Migrating from gentle-ai 3.7 SDD profiles

```sh
oc-profiles migrate   # backs up the old store to <store>.bak-v1-<time>
oc-profiles apply     # generates ODD agents, removes the retired SDD ones once
```

The mapping is orchestrator ← `sdd-orchestrator-P`, explore ←
`sdd-explore-P`, worker ← `sdd-apply-P`, verify ← `sdd-verify-P`, judges ←
`jd-judge-a/b-P`; the fix agent stays unassigned. The cleanup removes only
SDD agents and files that are provably unmodified gentle-ai output; anything
else is kept and reported as a warning.

### Creating a profile

TUI: `n` → name → assign models → "Create & Sync". The new
`odd-orchestrator-<name>` appears in OpenCode's agent list (Tab).

### Editing judges or the fix agent

TUI: select the profile, `enter`, `enter` → move to Judge A, Judge B or Fix
Agent → `enter` to pick a model, or `backspace` to fall back to the global
agent → "Continue" → "Save & Sync".

### Renaming

```sh
oc-profiles rename work work-2
```

or `r` in the TUI. Sessions bound to the old orchestrator name need to switch
to the new one.

### After `gentle-ai sync` or "Configure models"

```sh
oc-profiles sync
```

This regenerates any profile agents gentle-ai removed and refreshes every
profile from gentle-ai's updated ODD agents. Opening the TUI does the
regeneration automatically.

### Restoring from a backup

```sh
oc-profiles backup list
oc-profiles backup restore --seq 3 --yes   # without --seq: newest valid snapshot
```

or `b` in the TUI.

## Troubleshooting

### "refusing to overwrite agents not managed by oc-profiles"

```
refusing to overwrite agents not managed by oc-profiles:
  - profile work: agent "odd-worker-work" already exists and is not managed by oc-profiles (no [oc-profiles] marker in its description); rename or remove it, or rename the profile
```

An agent you wrote yourself sits at a key the profile needs. oc-profiles never
overwrites or deletes agents without `[oc-profiles]` in their description.
Rename or remove that agent, or rename the profile. Unmarked agents at keys
apply would delete are kept, with a `kept "<key>": not managed by oc-profiles`
warning.

### "store was written by a newer oc-profiles"

The store schema is newer than this binary. Upgrade oc-profiles; nothing is
touched in the meantime.

### Corrupt or missing store

- Read-only commands (`list`, `apply --dry-run`, `backup list`) report
  `store is corrupt: ...; run \`oc-profiles sync\` to restore it from backup snapshot #N (...)`
  and change nothing.
- `oc-profiles sync` (or any mutating command, or the TUI) restores it:
  `Recovered the store from backup snapshot #N (...); reason: ...`. The broken
  files are kept as `*.corrupt-<time>`.
- With no valid snapshot: `store is corrupt (...) and no valid backup snapshot exists in <dir>`.
  Inspect or delete the store files, then run `oc-profiles import`.

### "opencode.json changed during apply; re-run"

Another program (OpenCode, gentle-ai, an editor) wrote `opencode.json` while
apply was running. Nothing was written; run the command again.

### "validation failed, nothing written"

A generated agent delegates to an agent that doesn't exist, or a profile
orchestrator still names a gentle-ai ODD agent. This usually means gentle-ai's
`gentle-orchestrator` allows a task target that isn't defined; fix that (for
example with `gentle-ai sync`) and re-run.

### "BACKUP FAILED (the store and opencode.json are fine)"

The command did its work but couldn't write the backup mirror (permissions or
disk space in the backup dir). Fix the cause and run `oc-profiles sync`.

### Manual edits disappear

Profile agents are generated: every apply regenerates `odd-*-P`,
`jd-judge-*-P` and `jd-fix-agent-P` from gentle-ai's agents and the store.
Change models through oc-profiles instead of editing `opencode.json`.
