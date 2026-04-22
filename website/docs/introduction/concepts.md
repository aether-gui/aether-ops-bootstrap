---
id: concepts
title: Core concepts
sidebar_position: 2
---

# Core concepts

Before anything else, a shared vocabulary. These words mean specific things
throughout the docs, logs, and code.

## The three layers

The full system is a stack of three layers. This vocabulary is used
consistently — if you see "platform layer" in a log message, it means this:

```mermaid
block-beta
    columns 1
    block:cellular["Cellular Layer — UPF, AMF, SMF, NRF …"]
    end
    block:platform["Platform Layer — RKE2 + aether-ops (installed by bootstrap)"]
    end
    block:os["OS Layer — Ubuntu Server 22.04, 24.04, 26.04"]
    end

    cellular --> platform
    platform --> os

    style cellular fill:#f9e2af,stroke:#df8e1d,color:#000
    style platform fill:#89b4fa,stroke:#1d7af3,color:#000
    style os fill:#a6e3a1,stroke:#40a02b,color:#000
```

- **OS layer** — Ubuntu Server, versions 22.04, 24.04, and 26.04 (soon to be
  released). Kernel, base packages, networking stack. The operator installs
  this manually. The bootstrap assumes *nothing* about this layer beyond a
  supported Ubuntu version being present.
- **Platform layer** — RKE2 plus aether-ops, together with the OS-level
  prerequisites aether-ops needs (`git`, `make`, `ansible`) and the SSH / sudo
  configuration it relies on. **This is what `aether-ops-bootstrap` installs.**
- **Cellular layer** — the 4G/5G network functions (UPF, AMF, SMF, NRF, and so
  on) deployed *by* the platform layer. aether-ops owns this layer entirely;
  the bootstrap has no involvement.

Relationship verbs we use in the code and logs: the OS layer **hosts** the
platform layer; the platform layer **manages** the cellular layer; the
cellular layer **contains** network functions.

## Bootstrapping vs. running aether-ops

These are often confused; they are distinct.

| | `aether-ops-bootstrap` | `aether-ops` |
|---|---|---|
| **What it is** | A single-shot installer | A long-running service |
| **Runs** | Once per node (plus upgrades/repairs) | Continuously after bootstrap |
| **Owns** | The platform layer | The cellular layer |
| **Needs internet?** | No | Not typically |
| **Manages other nodes?** | No | Yes (via Ansible / its node agent) |

The bootstrap's responsibility begins at "a human just finished the Ubuntu
installer" and ends at "aether-ops is running and reachable, with RKE2
underneath it." Everything past that point is aether-ops' job: adding more
nodes, deploying SD-Core, distributing SSH keys, operating the cellular
workloads.

## The launcher and the bundle

The launcher (`aether-ops-bootstrap`, the binary) and the bundle
(`bundle.tar.zst`) are **two artifacts**, released together but versioned
independently.

- The **launcher** is code — it never changes at install time based on which
  bundle you pair it with. Upgrading the launcher is a new binary.
- The **bundle** is data — every `.deb`, every tarball, every template, plus
  a `manifest.json` that tells the launcher what's inside. Upgrading the
  bundle is a new tarball.

The `manifest.json` inside the bundle is the **contract** between them: it
declares a schema version the launcher checks before using the bundle. This
lets the two evolve at different rates without breaking each other.

See [The two artifacts](./the-two-artifacts.md) for the full picture.

## Components

Inside the launcher, the install / upgrade / repair logic is a **single loop
over ordered components**. Each component knows how to install or reconcile
one specific thing:

1. `debs` — installs the OS-level prerequisite `.deb` files.
2. `ssh` — writes sshd drop-ins.
3. `sudoers` — writes sudoers drop-ins.
4. `service_account` — creates the aether-ops service account and onramp user.
5. `rke2` — installs and starts RKE2.
6. `helm` — installs the Helm binary.
7. `onramp` — stages the aether-onramp and Helm chart repositories.
8. `aether_ops` — installs and starts the aether-ops daemon.

Each component implements the same interface: "what do you want? what's
installed? what would change?". Idempotency falls out naturally — if current
equals desired, the component is a no-op. See the
[Bootstrap Guide](/bootstrap-guide/components) for details.

## State

After a successful install, the launcher writes a JSON state file to
`/var/lib/aether-ops-bootstrap/state.json`. It records:

- the launcher version that ran,
- the bundle version and hash that was applied,
- per-component installed version,
- a history log of every action taken.

State is what makes `upgrade` and `repair` possible: the launcher reads state,
compares it to the new bundle's manifest, and applies only what changed.
See [state file](/bootstrap-guide/state-file).

## Airgap

"Airgap" in this project means one concrete rule: **the bootstrap never makes
a network request, ever.** Not to `apt-get update`, not to GitHub, not to a
container registry. Everything it needs is in the bundle, pre-downloaded and
checksummed on the build machine.

Network isolation is a correctness feature, not just a convenience for
disconnected environments. It makes bootstrap runs reproducible and auditable:
the same bundle on the same Ubuntu version produces the same result.

## Roles

In the 0.1.x line, the launcher accepts an optional `--roles` flag
(`mgmt`, `core`, `ran`) that filters which components run. This is a
transitional mechanism for exercising multi-node shapes before per-role
bundles land. See [roadmap](/bootstrap-guide/roadmap) for where this is going.

For a single-node install, omit `--roles`; every registered component runs.
