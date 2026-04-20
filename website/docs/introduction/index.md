---
id: index
title: What is aether-ops-bootstrap?
slug: /introduction
sidebar_position: 1
---

# What is aether-ops-bootstrap?

`aether-ops-bootstrap` turns a freshly installed Ubuntu Server into a running
**aether-ops management plane on top of RKE2** — without touching the network.

It exists to solve a specific, unglamorous problem: putting a working
Kubernetes-based management plane onto a machine that has no internet access,
no pre-installed dependencies beyond the Ubuntu essentials, and no operator
with the time to hand-install each piece.

## The problem

aether-ops manages 4G/5G network functions. Those functions run on edge
hardware that is frequently:

- **Airgapped.** No reachable apt mirrors, no GitHub, no container registries.
- **Regulated.** No unapproved packages, no PPAs, no curl-to-bash installers.
- **Field-deployed.** A technician brings a USB drive and expects "run one
  command, walk away" to work.

Before this project, standing up the aether-ops management plane in that
environment meant a tangle of custom scripts, Ansible runbooks, and "now copy
these five files into `/etc/rancher/rke2/` and don't forget to …". Those
approaches were brittle, inconsistent across sites, and impossible to audit.

## The shape of the solution

Two artifacts, released together:

- **A launcher binary** — `aether-ops-bootstrap`. Statically linked Go, single
  file, 10–30 MB. Contains all the logic: preflight checks, `.deb` installation,
  systemd units, RKE2 install, aether-ops install, state tracking,
  reconciliation. **Versioned with semver** (e.g. `v0.1.43`).
- **An offline bundle** — `bundle.tar.zst`. An opaque tarball containing every
  `.deb`, every RKE2 artifact, the Helm binary, the aether-ops binary, and
  every template the launcher needs. **Versioned with calver** (e.g.
  `2026.04.1`).

The operator copies both files to the target host and runs:

```bash
./aether-ops-bootstrap install --bundle bundle.tar.zst
```

Minutes later, RKE2 is running, aether-ops is reachable, and the bootstrap
writes a state file and exits. The bootstrap never runs again on that host
except to **upgrade** (new bundle) or **repair** (fix drift).

## Who this is for

Reading this as someone who:

- **Operates the install** — You have Ubuntu/Linux fluency, know what `apt`,
  `systemd`, and `sudo` are, but have never touched RKE2 or aether-ops. Start
  with [Getting Started](/getting-started).
- **Builds the bundle** — You maintain the release process, edit `bundle.yaml`,
  care about reproducibility. Go to [Build Guide](/build-guide).
- **Extends the launcher** — You want to add components, understand the state
  machine, or troubleshoot a failed install. Go to [Bootstrap Guide](/bootstrap-guide).

## How to read these docs

Documentation is split into four sections that stand alone:

1. **[Introduction](/introduction)** — what it is, how it works, what the
   vocabulary means. (You're here.)
2. **[Getting Started](/getting-started)** — the shortest path from "I have
   artifacts and a fresh Ubuntu box" to "aether-ops is running."
3. **[Build Guide](/build-guide)** — everything about producing bundles.
4. **[Bootstrap Guide](/bootstrap-guide)** — everything about the launcher,
   its commands, and what happens on the target host.

Use the sidebar or search — every page is reachable from anywhere.

:::note v0.1.x Alpha
This is the first public documentation for the 0.1.x Alpha line.
Behaviors marked **"roadmap"** are not in 0.1.x. The code and this site
will move quickly; expect churn on `main` until a stable 0.2 / 1.0 cut.
:::
