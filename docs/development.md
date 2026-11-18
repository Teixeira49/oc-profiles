# Development

## Requirements

- Go (the version in `go.mod`, currently 1.27.4)
- macOS or Linux
- Optional: `opencode` on `PATH` to try the TUI's model picker

## Build and test

| Command | What it does |
| --- | --- |
| `make build` | Build `bin/oc-profiles`, stamping `main.version` from `git describe` (leading `v` stripped) |
| `make test` | `go test ./...` |
| `make vet` | `go vet ./...` |
| `make install` | Build and install to `$(PREFIX)` (default `~/.local/bin`) |
| `make uninstall` | Remove the installed binary only |
| `make clean` | Remove `bin/` |

CI (`.github/workflows/ci.yml`) runs on pushes and pull requests to `main`, on
Ubuntu and macOS: `go mod verify`, a `gofmt -l` check, `go vet ./...`,
`go test -race -count=1 ./...` and `go build ./...`. Run the same locally
before opening a PR:

```sh
gofmt -l . && go vet ./... && go test -race -count=1 ./...
```

## Trying it safely

Never point experiments at your real config. Every path is injectable:

```sh
export OC_PROFILES_OPENCODE_DIR=/tmp/oc-try/opencode
export OC_PROFILES_HOME=/tmp/oc-try/store
export OC_PROFILES_BACKUP_DIR=/tmp/oc-try/backup
mkdir -p "$OC_PROFILES_OPENCODE_DIR" && cp ~/.config/opencode/opencode.json "$OC_PROFILES_OPENCODE_DIR/"
go run ./cmd/oc-profiles import
go run ./cmd/oc-profiles apply --dry-run
```

## Conventions

- **Language**: code, comments, UI text and docs in English.
- **Commits**: [Conventional Commits](https://www.conventionalcommits.org/)
  (`feat:`, `fix:`, `docs:`, `test:`, `chore:`, `ci:`). The release changelog
  groups `feat` and `fix`, and leaves out `docs`, `test`, `chore` and `ci`.
- **Tests**: every behaviour change comes with a regression test. Tests use
  `t.TempDir()` and injected `core.Paths`; they must never touch the real
  home directory. TUI tests drive `Model.Update` with key messages.
- **Safety rules** in `internal/core`: never write `opencode.json` without a
  backup, the validation gate and the concurrency guard; never overwrite or
  delete an agent without the `[oc-profiles]` marker; write files only through
  `writeFileAtomic`.
- **Formatting**: `gofmt`. No other linters are required.
- **Dependencies**: keep them minimal (Bubble Tea and Lip Gloss only).

## Cutting a release

Releases are built by GoReleaser (`.goreleaser.yaml`, v2 schema) from
`.github/workflows/release.yml` when a `v*` tag is pushed:

```sh
git tag -a v0.2.0 -m "v0.2.0"
git push origin v0.2.0
```

The workflow runs the tests, then builds `darwin` and `linux` for `amd64` and
`arm64` with `CGO_ENABLED=0` and `-X main.version=<version>`. It publishes
`oc-profiles_<version>_<os>_<arch>.tar.gz` archives (binary, LICENSE, NOTICE,
README.md), `checksums.txt` (sha256) and a changelog built from the commits.

Check the configuration locally:

```sh
go run github.com/goreleaser/goreleaser/v2@latest check
go run github.com/goreleaser/goreleaser/v2@latest release --snapshot --clean   # writes dist/; delete it afterwards
```

## Contributing

1. Open an issue describing the problem or feature first.
2. Branch from `main`, keep changes focused, add tests.
3. Make sure the CI commands above pass.
4. Open a pull request with a Conventional Commit title.

Portions of the TUI and the name rules are adapted from gentle-ai (MIT); keep
`NOTICE` up to date when adapting more code.
