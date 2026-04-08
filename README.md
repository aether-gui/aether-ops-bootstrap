# aether-ops-bootstrap

`aether-ops-bootstrap` takes a freshly installed Ubuntu Server host and produces a running aether-ops management plane on top of RKE2. It runs once per management node, requires no internet access, and hands off to aether-ops for all further configuration.

Two artifacts are produced: a statically linked Go launcher binary and an offline payload bundle (`aether-ops-bundle-<version>-linux-<arch>.tar.zst`). Together they bring up the platform layer — RKE2, aether-ops, and all OS-level prerequisites — without touching the network.

See [DESIGN.md](DESIGN.md) for full architecture and design details.

## How It Works

### Building the Bundle

The `build-bundle` tool reads a declarative spec (`bundle.yaml`) and assembles an offline payload containing all dependencies — no manual downloading or packaging.

```mermaid
flowchart LR
    spec["bundle.yaml<br/><i>human-edited spec</i>"]
    resolve["Resolve"]
    fetch["Fetch"]
    verify["Verify"]
    lock["Lock"]
    stage["Stage"]
    assemble["Assemble"]
    lockfile["bundle.lock.json"]
    tarball["bundle.tar.zst"]
    manifest["manifest.json"]

    spec --> resolve --> fetch --> verify --> lock --> stage --> assemble
    lock --> lockfile
    assemble --> tarball
    assemble --> manifest
```

### Bootstrapping a Host

The launcher binary reads the bundle, walks each component in dependency order, and brings the host from bare Ubuntu to a running aether-ops management plane.

```mermaid
flowchart TD
    start(["aether-ops-bootstrap install"])
    preflight["Preflight<br/><i>OS version, arch, disk, RAM</i>"]
    debs["Install .debs<br/><i>git, make, ansible + deps</i>"]
    ssh["Configure SSH<br/><i>sshd drop-ins, keypair</i>"]
    sudoers["Configure sudoers<br/><i>drop-in for service account</i>"]
    svcacct["Create service account<br/><i>useradd, groupadd</i>"]
    rke2["Install RKE2<br/><i>extract, config, systemd, wait</i>"]
    aetherops["Install aether-ops<br/><i>binary, config, systemd, wait</i>"]
    done(["Handoff complete"])

    start --> preflight --> debs --> ssh --> sudoers --> svcacct --> rke2 --> aetherops --> done
```

Each component follows the same **Plan/Apply** pattern: compare current state to the bundle's desired state, compute what needs to change, then apply it. If nothing changed, the component is a no-op — making every run idempotent.

## Quick Start

```bash
make build
```

Produces `dist/aether-ops-bootstrap-linux-amd64` and `dist/aether-ops-bootstrap-linux-arm64`.

```bash
./dist/aether-ops-bootstrap-linux-amd64 version
./dist/aether-ops-bootstrap-linux-amd64 install
```

## Development

```bash
make test          # run tests with race detector and coverage
make vet           # go vet
make lint          # golangci-lint (install with: make install-lint)
make clean         # remove build artifacts
```
