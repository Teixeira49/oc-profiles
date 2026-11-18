# oc-profiles

[![CI](https://github.com/Teixeira49/oc-profiles/actions/workflows/ci.yml/badge.svg)](https://github.com/Teixeira49/oc-profiles/actions/workflows/ci.yml)
[![Release](https://github.com/Teixeira49/oc-profiles/actions/workflows/release.yml/badge.svg)](https://github.com/Teixeira49/oc-profiles/actions/workflows/release.yml)
[![Latest release](https://img.shields.io/github/v/release/Teixeira49/oc-profiles)](https://github.com/Teixeira49/oc-profiles/releases/latest)

A CLI/TUI to create, edit, rename and delete **ODD profiles** for
[OpenCode](https://opencode.ai) on top of
[gentle-ai](https://github.com/Gentleman-Programming/gentle-ai).

A profile `P` is a set of clones of gentle-ai's live ODD agents, each with its
own model and reasoning effort:

| Role | Generated agent | Cloned from |
| --- | --- | --- |
| Orchestrator (primary, visible with Tab) | `odd-orchestrator-P` | `gentle-orchestrator` |
| Explore / Worker / Verify | `odd-explore-P`, `odd-worker-P`, `odd-verify-P` | `gentle-ai-explore`, `gentle-ai-worker`, `gentle-ai-verify` |
| Judge A / B (optional) | `jd-judge-a-P`, `jd-judge-b-P` | `jd-judge-a`, `jd-judge-b` |
| Fix Agent (optional) | `jd-fix-agent-P` | `jd-fix-agent` |

## Why

gentle-ai 4.0 retired its "OpenCode SDD Profiles" menu, and `gentle-ai sync`
may remove agents it doesn't own. oc-profiles keeps your model assignments in
its own store (`~/.config/oc-profiles/`) and regenerates the profile agents
from gentle-ai's **live** agents, so upstream prompt improvements reach every
profile. Generated agents carry an `[oc-profiles]` marker in their description,
which keeps gentle-ai's sync from claiming them. The menu looks and behaves
like gentle-ai 3.7's profile menu.

## Requirements

- OpenCode with gentle-ai 4.x installed for it (the `gentle-orchestrator` and
  `gentle-ai-*` agents must exist in `~/.config/opencode/opencode.json`).
- `opencode` on `PATH` (used to list models), or `OC_PROFILES_OPENCODE_BIN`.
- Go 1.27+ only to build from source.
- macOS or Linux.

## Install

From a release (pick the version, OS and arch from the
[releases page](https://github.com/Teixeira49/oc-profiles/releases)):

```sh
curl -sSL https://github.com/Teixeira49/oc-profiles/releases/download/v0.4.0/oc-profiles_0.4.0_darwin_arm64.tar.gz \
  | tar -xz oc-profiles
install -m 0755 oc-profiles ~/.local/bin/
```

With Go:

```sh
go install github.com/Teixeira49/oc-profiles/cmd/oc-profiles@latest
```

From source: `make install` (to `~/.local/bin`, or `make install PREFIX=/opt/homebrew/bin`).

## Quickstart

```sh
oc-profiles import     # first run: build the store from opencode.json
oc-profiles            # open the TUI: n new, enter/e edit, r rename, d delete, s sync
oc-profiles sync       # after every `gentle-ai sync` or "Configure models"
```

Upgrading from gentle-ai 3.7 SDD profiles: run `oc-profiles migrate`, then
`oc-profiles apply`.

## Commands

| Command | What it does |
| --- | --- |
| `oc-profiles` | Open the TUI |
| `oc-profiles import [--force [--allow-empty]]` | Build the store from the profiles in `opencode.json` |
| `oc-profiles migrate` | Convert a v1 (gentle-ai 3.7 SDD) store to ODD profiles |
| `oc-profiles apply [--dry-run]` | Re-apply the store to `opencode.json` |
| `oc-profiles sync` | Recover the store if needed, re-apply, write a backup snapshot |
| `oc-profiles rename OLD NEW [--no-apply]` | Rename a profile, keeping its models |
| `oc-profiles list` | List profiles and their orchestrator model |
| `oc-profiles backup list` | List backup snapshots |
| `oc-profiles backup restore [--seq N] --yes` | Restore the store from a snapshot, then sync |
| `oc-profiles version` | Print the version (also `--version`) |

## Documentation

- [Usage](docs/usage.md): TUI walkthrough, workflows, troubleshooting
- [Commands](docs/commands.md): full CLI reference, flags, environment, exit codes
- [Architecture](docs/architecture.md): how the code works and why
- [Development](docs/development.md): build, test, release, contributing

## License

MIT, see [LICENSE](LICENSE). Portions adapted from
[gentle-ai](https://github.com/Gentleman-Programming/gentle-ai) v3.7.0 (MIT),
see [NOTICE](NOTICE).
