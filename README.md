# aether-ops-bootstrap

`aether-ops-bootstrap` takes a freshly installed Ubuntu Server host and produces a running aether-ops management plane on top of RKE2. It runs once per management node, requires no internet access, and hands off to aether-ops for all further configuration.

Two artifacts are produced: a statically linked Go launcher binary and an offline payload bundle (`bundle.tar.zst`). Together they bring up the platform layer — RKE2, aether-ops, Helm, and all OS-level prerequisites — without touching the network.

## Documentation

Full user-facing documentation lives at
**<https://aether-gui.github.io/aether-ops-bootstrap/>**.

| | |
|---|---|
| Installing a pre-built bundle | [Getting Started](https://aether-gui.github.io/aether-ops-bootstrap/getting-started) |
| Building bundles from `bundle.yaml` | [Build Guide](https://aether-gui.github.io/aether-ops-bootstrap/build-guide) |
| Launcher reference, components, state | [Bootstrap Guide](https://aether-gui.github.io/aether-ops-bootstrap/bootstrap-guide) |
| Project concepts and architecture | [Introduction](https://aether-gui.github.io/aether-ops-bootstrap/introduction) |

Internal design docs (historical / design-of-record):
[`DESIGN.md`](DESIGN.md), [`MULTI-NODE-DESIGN.md`](MULTI-NODE-DESIGN.md).

## Quick build

```bash
make build          # → dist/aether-ops-bootstrap (launcher)
make bundle         # → dist/bundle.tar.zst (offline payload)
make package        # → dist/aether-ops-bootstrap-<version>.tar.gz (everything)
```

See the [Build Guide](https://aether-gui.github.io/aether-ops-bootstrap/build-guide/building-locally) for details, prerequisites, and multi-bundle mode.

## Development

```bash
make test           # go test -race -cover ./...
make vet            # go vet ./...
make lint           # golangci-lint (install with: make install-lint)
make build-all      # both launcher and bundle tool
make clean          # remove build artifacts
```

### End-to-end tests

VM-based tests under `tests/` use [DART](https://github.com/bgrewell/dart) and LXD:

```bash
make test-e2e-bootstrap        # single-node bootstrap (~10-15 min)
make test-e2e-multi-bootstrap  # multi-node bootstrap
make test-e2e-deploy           # single-node full deploy (~30-45 min)
make test-e2e-multi-deploy     # multi-node full deploy
make test-e2e-quick            # both bootstrap suites
make test-e2e                  # all four suites
```

### Docs site

```bash
make docs           # dev server at http://localhost:3000
make docs-build     # production build in website/build
```

The site is published to GitHub Pages by `.github/workflows/docs.yml` on every push to `main` and every `v*` tag.

## CI/CD

Three workflows in `.github/workflows/`:

- **`launcher.yml`** — vet, lint, test, build, vuln scan on every push and PR.
- **`release.yaml`** — on `v*` tag: GoReleaser, bundle build, SBOMs, vulnerability scans, upload to GitHub release.
- **`docs.yml`** — docs site build on PRs (build-only) and deploy on `main` / `v*` tag.
