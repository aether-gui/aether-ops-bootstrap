# Plan: aether-ops-bootstrap Repository Skeleton

## Context

The repo currently has `README.md` (design overview) and `DESIGN-PLANNING.md` (detailed design). The goal is to create the full Go repository skeleton per the instructions: directory layout, Go module, compiling stub packages, placeholder CLIs, bundle spec, and CI workflows. No real logic — just a compilable, testable foundation.

## Inconsistencies to Fix in DESIGN-PLANNING.md

Before creating files, update DESIGN-PLANNING.md to resolve inconsistencies between it and the instructions:

1. **Component list mismatch**: DESIGN-PLANNING.md line 106 lists `service_account` as a component, but the instructions' directory layout omits it. Add `internal/components/serviceaccount/` to the directory layout in DESIGN-PLANNING.md to match the component list (keeping both sources consistent).
2. **Module path**: The instructions say `github.com/grewell/aether-ops-bootstrap` but the actual git remote is `github.com/aether-gui/aether-ops-bootstrap`. Update any module path references in DESIGN-PLANNING.md to use `github.com/aether-gui/aether-ops-bootstrap`.
3. **Repository layout in DESIGN-PLANNING.md** (line 229+): Missing `deb/` and `state/` package descriptions in the layout section — add them to match the actual skeleton structure from the instructions.

## Key Decisions

- **Module path**: `github.com/aether-gui/aether-ops-bootstrap` (matches git remote)
- **Go version**: 1.22
- **DESIGN.md**: Copy `DESIGN-PLANNING.md` content to `DESIGN.md` (with fixes applied)
- **README.md**: Replace current content with short intro + link to DESIGN.md + build instructions
- **Dependencies**: stdlib only (no cobra, no external deps)
- **CLI dispatch**: `flag` package + manual `os.Args` subcommand dispatch
- **`service_account` component**: Include it (present in DESIGN-PLANNING.md component order)

## Constraints (must hold from day one)

- Go 1.22+, `CGO_ENABLED=0`, pure Go, no shell-outs except documented exception for `dpkg`, `useradd`, `groupadd`
- `internal/bundle` is imported by both `cmd/aether-ops-bootstrap` and `cmd/build-bundle`
- Every package compiles and `go test ./...` passes
- `go vet ./...` and `golangci-lint run` clean
- No network calls in the launcher

## Files to Create (~45 files)

### Phase 1: Foundation

| File | Description |
|------|-------------|
| `go.mod` | Module `github.com/aether-gui/aether-ops-bootstrap`, go 1.22 |
| `DESIGN.md` | Copy of DESIGN-PLANNING.md with inconsistencies fixed |
| `README.md` | New short intro, link to DESIGN.md, `make build` instructions |
| `.gitignore` | `dist/`, `build/`, `docs/generated/`, `*.tar.zst`, `*.deb`, editor files |
| `.golangci.yml` | Enable govet, staticcheck, errcheck, ineffassign, unused, gofmt, goimports |
| `Makefile` | Targets: build, build-bundle, test, lint, vet, clean; cross-compile linux/amd64+arm64 |
| `bundle.yaml` | Placeholder spec per instructions |
| `bundle.lock.json` | `{}` |
| `templates/.gitkeep` | Empty |
| `docs/conceptual/DESIGN.md` | Symlink → `../../DESIGN.md` |
| `docs/generated/.gitkeep` | Empty (gitignored dir) |
| `test/integration/.gitkeep` | Empty |

### Phase 2: Internal Packages (dependency order)

**`internal/bundle/manifest.go`** — Manifest types:
- `Manifest` struct with `SchemaVersion`, `BundleVersion`, `BundleSHA256`, `BuildInfo`, `Components`
- `BuildInfo`, `ComponentList`, per-component entry structs (`DebEntry`, `RKE2Entry`, `AetherOpsEntry`)
- `Read(path) (*Manifest, error)` and `Write(path, *Manifest) error` stubs
- `SchemaVersion = 1` constant

**`internal/state/state.go`** — Runtime state types:
- `State` struct: `SchemaVersion`, `LauncherVersion`, `BundleVersion`, `BundleHash`, `Components` map, `History` array
- `ComponentState`: `Version`, `ConfigHash`, `InstalledAt`
- `HistoryEntry`: `Action`, `Timestamp`, `LauncherVersion`, `BundleVersion`
- `Read`/`Write` stubs

**`internal/components/component.go`** — Component interface per design doc:
```go
type Component interface {
    Name() string
    DesiredVersion(b *bundle.Manifest) string
    CurrentVersion(s *state.State) string
    Plan(current, desired string) (Plan, error)
    Apply(ctx context.Context, plan Plan) error
}
```
Plus `Plan` struct stub.

**Component sub-packages** (each implements the interface, returns "not implemented" errors):
- `internal/components/debs/debs.go`
- `internal/components/ssh/ssh.go`
- `internal/components/sudoers/sudoers.go`
- `internal/components/serviceaccount/serviceaccount.go`
- `internal/components/rke2/rke2.go`
- `internal/components/aetherops/aetherops.go`

**`internal/deb/deb.go`** — Placeholder: `Package` struct, `Parse` stub

**`internal/systemd/systemd.go`** — Placeholder: `Start`, `Stop`, `Enable`, `Status` stubs

### Phase 3: Commands

**`cmd/aether-ops-bootstrap/main.go`**:
- Subcommands: `install`, `upgrade`, `repair`, `check`, `state`, `version`
- Each prints "not implemented" and exits 0
- `version` prints version string (embedded via `-ldflags "-X main.version=..."`, default `"dev"`)

**`cmd/build-bundle/main.go`**:
- Flags: `--spec` (default `bundle.yaml`), `--lock` (default `bundle.lock.json`), `--output` (default `dist/`)
- Prints "not implemented" and exits 0

### Phase 4: Tests

One `_test.go` per internal package (trivial tests):
- `internal/bundle/manifest_test.go`
- `internal/state/state_test.go`
- `internal/components/component_test.go`
- `internal/components/debs/debs_test.go`
- `internal/components/ssh/ssh_test.go`
- `internal/components/sudoers/sudoers_test.go`
- `internal/components/serviceaccount/serviceaccount_test.go`
- `internal/components/rke2/rke2_test.go`
- `internal/components/aetherops/aetherops_test.go`
- `internal/deb/deb_test.go`
- `internal/systemd/systemd_test.go`

### Phase 5: CI Workflows

- `.github/workflows/launcher.yml` — Build, test, vet, lint on push/PR
- `.github/workflows/bundle.yml` — Run `go run ./cmd/build-bundle` (no-op for now)
- `.github/workflows/integration.yml` — Placeholder echoing "TODO: VM-based integration test"

## Import Dependency Graph (no cycles)

```
bundle (stdlib only)
state (stdlib only)
deb (stdlib only)
systemd (stdlib only)
components/component.go → bundle, state
components/{debs,ssh,sudoers,serviceaccount,rke2,aetherops} → bundle, state
cmd/aether-ops-bootstrap → fmt, os, flag
cmd/build-bundle → fmt, flag
```

## Verification (Acceptance Criteria)

1. `go build ./...` — all packages compile
2. `go test ./...` — all stub tests pass (at least one `TestFoo` per internal package)
3. `go vet ./...` — clean
4. `make build` — produces `./dist/aether-ops-bootstrap-linux-amd64`
5. `./dist/aether-ops-bootstrap-linux-amd64 version` — prints version string
6. `./dist/aether-ops-bootstrap-linux-amd64 install` — prints "not implemented", exits 0
7. `go run ./cmd/build-bundle --spec bundle.yaml` — prints "not implemented", exits 0
8. `DESIGN.md` present at repo root and referenced from `README.md`

## Non-goals for This Pass

- No real .deb parsing
- No real RKE2 install logic
- No real systemd D-Bus calls
- No real manifest generation
- No real bundle tarball creation
- No integration tests against real VMs
