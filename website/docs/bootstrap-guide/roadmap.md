---
id: roadmap
title: Roadmap
sidebar_position: 8
---

# Roadmap

What's coming after 0.1.x. This page summarizes
[`MULTI-NODE-DESIGN.md`](https://github.com/aether-gui/aether-ops-bootstrap/blob/main/MULTI-NODE-DESIGN.md)
in the repo; consult that file for the full design discussion.

:::note 0.1.x = single-node + transitional `--roles`
Everything below is **not** in 0.1.x Alpha. The 0.1.x line is a single-node
bootstrap with an optional `--roles` flag for exercising component subsets.
:::

## The core principle

**Bootstrap owns single-node provisioning. aether-ops owns multi-node
orchestration.**

The bootstrap's scope is deliberately narrow: take a bare Ubuntu host and
make it ready for its role. The bundle declares the role. Multi-node
coordination — pushing bundles, monitoring health, integrating into a
fleet — lives in aether-ops, which has a node agent in progress.

## The revised role model

Production deployments have three distinct node types:

| Role | What it runs | K8s? | Key components |
|---|---|---|---|
| **Management** | aether-ops only | No | `debs`, `ssh`, `sudoers`, `service_account`, `aether_ops` |
| **SD-Core** | RKE2 server + 4G/5G core NFs | Yes (server) | `debs`, `ssh`, `sudoers`, RKE2 server, Helm, charts, images |
| **gNB (current)** | Docker + gNB radio software | No | `debs`, `ssh`, `sudoers`, Docker, gNB images |
| **gNB (future)** | RKE2 agent + gNB | Yes (agent) | `debs`, `ssh`, `sudoers`, RKE2 agent, gNB images |

The important shift from today: **the management node does not need RKE2
or Helm in multi-node mode.** Those move to SD-Core. A single-node
"all-in-one" spec will remain available for demos and testing.

## Phased roadmap

### Phase 1 — Multi-role specs + new components

- Extract today's `bundle.yaml` into `specs/management.yaml` (slim: no
  RKE2 / Helm).
- Write `specs/sdcore.yaml` (RKE2 server, Helm, SD-Core charts + images).
- Write `specs/gnb.yaml` (Docker, gNB images, OS tuning).
- Add new components: `docker`, `os_tuning`, `images`.
- Add agent mode to the existing `rke2` component (joins an existing
  cluster using a join token).
- **Already in place:** the builder's `--spec <dir>` multi-spec mode.

**Outcome.** Manual multi-role deployment: operator builds per-role
bundles, carries each to the right host, runs `install`.

### Phase 2 — Depot staging

- Management spec gains a `depot:` section declaring role sub-bundles.
- New `depot` component unpacks sub-bundles plus a copy of the launcher
  to `/var/lib/aether-ops/depot/` on the management node.
- aether-ops gains a "bootstrap node" workflow: push bundle from depot,
  run launcher remotely, monitor.

**Outcome.** Automated multi-node provisioning from the aether-ops UI.
One manual bootstrap of the management node, then everything else is
driven from there.

### Phase 3 — Node agent integration (aether-ops work)

- aether-ops' node agent pulls its role bundle from the depot on first
  contact.
- Agent runs the launcher locally, reports status back.

**Outcome.** Self-service provisioning at hundreds-of-gNBs scale.

### Future — gNB migration to Kubernetes

When gNBs move from Docker to Kubernetes, `specs/gnb.yaml` switches its
component set from `docker` to `rke2` (agent mode). The RKE2 agent joins
SD-Core's cluster using a join token; token distribution becomes
aether-ops' responsibility.

## What the `--roles` flag is (today) and isn't (long-term)

The 0.1.x `--roles` flag (`mgmt`, `core`, `ran`) is a **transitional
mechanism** for exercising multi-node shapes before per-role bundles
land. It works by filtering which components register on a given run.

In the long run, per-role bundles replace the flag: the bundle *is* the
role. If a bundle doesn't include RKE2, the `rke2` component has nothing
to do and doesn't run — no flag needed. Today's `--roles` filtering will
either disappear or become a no-op once Phase 1 ships.

Existing scripts that pass `--roles` should continue to work until the
flag is formally deprecated (a future 0.x.y release will announce the
change).

## Approaches considered but not chosen

From `MULTI-NODE-DESIGN.md`:

- **Role-tagged composite bundle.** One fat bundle with a `--role` flag
  selecting components. Simple but wasteful at scale and no automation
  story. Rejected.
- **Site manifest + orchestrator.** A `site.yaml` describing every node
  with a new orchestrator command driving bootstrap across the fleet.
  Duplicates what aether-ops should own. Rejected.
- **Golden image factory.** Build-time bootstrap inside VMs, deploy by
  flashing/PXE. Interesting for hundreds of identical gNBs on standardized
  hardware. Deferred — heavy infrastructure, complex upgrade story.
- **Bundle layers (OCI-style).** Shared base + role deltas,
  content-addressed. Over-engineered given bundle size isn't currently a
  constraint.

## Open design questions

Tracked in the repo in `DESIGN.md` and `MULTI-NODE-DESIGN.md`; summarized
here for convenience.

- **First-run setup UX for aether-ops.** Initial admin credential flow:
  does the bootstrap print it, or does aether-ops start in a "first-run
  wizard" mode with a one-time setup URL? Affects what "done" looks like.
- **Bundle signing format.** GPG vs. cosign. 0.1.x ships unsigned; a
  future release picks one.
- **Host `fapolicyd` support.** Ubuntu doesn't use it by default, but
  hardened derivatives might. Decide whether to detect and configure, or
  document as out of scope.
- **Single-node vs multi-node management spec.** One `management.yaml`
  that optionally includes RKE2, or two separate specs? Today's single-node
  spec is useful for demos.
- **Join token flow for gNBs and additional SD-Core nodes.** aether-ops
  API, depot metadata file, or env var to the launcher?
- **SD-Core HA.** Will there be multiple SD-Core nodes forming a K8s HA
  cluster? If so, first node is RKE2 server init; subsequent ones are
  RKE2 server join — different configs from the same bundle.
- **Depot format.** Flat filesystem directory (simple, SCP-friendly) vs
  HTTP server (enables pull-based agent). Could start as filesystem and
  add HTTP later.
