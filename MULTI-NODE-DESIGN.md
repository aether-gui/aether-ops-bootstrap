# Multi-Node, Multi-Role Bootstrap: Design Discussion

> **Looking for user-facing docs?** See
> <https://aether-gui.github.io/aether-ops-bootstrap/bootstrap-guide/roadmap>
> for a condensed, published version. This file is the internal design-of-record
> and contains the full phased roadmap and approaches-considered discussion.


## Context

The bootstrap system today handles one role: take a clean Ubuntu server and produce a running management plane (RKE2 + aether-ops). Real deployments need multiple node types with different software stacks.

**Scale target:** Currently small (3-5 nodes), designing for hundreds of gNBs. Bundle size is not a constraint.

---

## Revised Role Model

In multi-node deployments, the roles differ from what the current single-node bootstrap assumes:

| Role | What it runs | K8s? | Key components |
|------|-------------|------|----------------|
| **Management** | aether-ops only | No | debs, SSH/sudo, service account, aether-ops |
| **SD-Core** | RKE2 server + 4G/5G core NFs | Yes (server) | debs, SSH/sudo, RKE2 server, Helm, SD-Core charts + images |
| **gNB (current)** | Docker + gNodeB radio software | No | debs, SSH/sudo, Docker, gNB images |
| **gNB (future)** | RKE2 agent + gNodeB | Yes (agent, joins SD-Core cluster) | debs, SSH/sudo, RKE2 agent, gNB images |

**Important shifts from current design:**
- The management node does NOT need RKE2 or Helm -- that moves to SD-Core
- SD-Core owns the k8s cluster (RKE2 server mode)
- gNBs are currently Docker-only, planned to migrate to k8s agents joining the SD-Core cluster
- The current bootstrap installs too much on the management node for the multi-node case

---

## Core Principle

**Bootstrap owns single-node provisioning. Aether-ops owns multi-node orchestration.**

Bootstrap takes a bare Ubuntu host and makes it ready for its role. The bundle defines the role (no `--role` flag needed). Multi-node coordination (push bundles, monitor health, integrate into fleet) belongs to aether-ops, which has a node agent in progress.

---

## Recommended Architecture

### The flow

```
┌─────────────────────────────────────────────────────────┐
│ BUILD TIME                                              │
│                                                         │
│  specs/                                                 │
│    management.yaml  ──→ management-2026.04.1.tar.zst    │
│    sdcore.yaml      ──→ sdcore-2026.04.1.tar.zst        │
│    gnb.yaml         ──→ gnb-2026.04.1.tar.zst           │
│                                                         │
│  management bundle optionally includes the others       │
│  in a depot/ directory                                  │
└─────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────┐
│ DEPLOY: MANAGEMENT NODE (manual, one time)              │
│                                                         │
│  $ aether-ops-bootstrap install --bundle mgmt.tar.zst   │
│                                                         │
│  Installs: debs, SSH/sudo, service account, aether-ops  │
│  Stages: depot/ with role bundles + launcher binary      │
│  Result: aether-ops running, depot available             │
│  NOTE: no RKE2, no Helm on this node                    │
└─────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────┐
│ DEPLOY: SD-CORE + gNB NODES (aether-ops driven)        │
│                                                         │
│  Operator adds nodes in aether-ops UI, assigns roles    │
│  Aether-ops (via node agent or Ansible):                │
│    1. Copies launcher + role bundle to target node      │
│    2. Runs: aether-ops-bootstrap install --bundle ...   │
│    3. Monitors health, integrates into inventory        │
│                                                         │
│  SD-Core: gets RKE2 server + Helm + charts + images     │
│  gNB: gets Docker + gNB images (or RKE2 agent later)   │
└─────────────────────────────────────────────────────────┘
```

### What changes in the bootstrap codebase

**1. Role-specific specs and new components**

New component types needed:
- `docker` -- install Docker/containerd from vendored debs, configure, start
- `os_tuning` -- sysctl, hugepages, CPU isolation for gNB/SD-Core performance
- `rke2` component gains agent mode (config points at SD-Core server, uses join token)
- `images` component -- stage container images into Docker or k8s image store

The launcher itself needs no `--role` flag because the bundle defines what components to install. The existing component registry + manifest system handles this naturally: if the manifest has no RKE2 entry, the RKE2 component doesn't register.

**2. Depot staging**

The management bundle's spec gains a `depot` section:
```yaml
depot:
  bundles:
    - specs/sdcore.yaml
    - specs/gnb.yaml
  include_launcher: true
```

A new `depot` component unpacks these to `/var/lib/aether-ops/depot/`. Aether-ops reads from there.

**3. Management role slimming**

For multi-node deployments, the management spec drops RKE2 and Helm. This might mean two management specs (`management-standalone.yaml` for single-node, `management.yaml` for multi-node) or a single spec with optional components.

---

## Approaches Considered

| # | Approach | Summary | Verdict |
|---|----------|---------|---------|
| 1 | Role-Tagged Composite | One fat bundle, `--role` flag selects components | Simple but wasteful at scale |
| 2 | Multi-Spec Build | One spec per role, right-sized bundles | **Foundation layer -- use this** |
| 3 | Bundle Depot | Mgmt carries sub-bundles, aether-ops pushes | **Phase 2 -- natural fit with aether-ops agent** |
| 4 | Site Manifest + Orchestrator | New orchestrator drives all nodes from one control point | Duplicates aether-ops' role |
| 5 | Golden Image Factory | Build-time bootstrap inside VMs, deploy by flashing | Future optimization for gNBs at massive scale |
| 6 | Bundle Layers (OCI-style) | Shared base + role deltas, content-addressed dedup | Over-engineered given size isn't a constraint |
| 7 | Pull-Based Agent + Depot | HTTP depot on mgmt, nodes self-bootstrap | Converges with aether-ops node agent |

### Approach details

**1. Role-Tagged Composite Bundle** -- One fat bundle contains all roles. Launcher gets `--role <name>` flag, filters which components to apply. Simple but every node carries everyone's artifacts. No automation story. Doesn't scale to hundreds of gNBs.

**2. Multi-Spec Build** -- Maintain `specs/management.yaml`, `specs/sdcore.yaml`, `specs/gnb.yaml`. The builder (which already supports `--spec <dir>`) produces right-sized bundles per role. Almost free to implement. Each role gets exactly what it needs.

**3. Bundle Depot** -- Bootstrap management node first. Its bundle contains role bundles in a `depot/` directory. Aether-ops pushes bundles to other nodes via its Ansible/agent infrastructure. One manual step, then aether-ops automates the rest. Natural fit given the planned node agent.

**4. Site Manifest + Orchestrator** -- A `site.yaml` describes every node, role, and IP. A new orchestrator command drives bootstrap across all nodes from one control point. Powerful but duplicates what aether-ops should own. Only makes sense if bootstrap must be fully self-contained.

**5. Golden Image Factory** -- Run bootstrap at build time inside VMs to produce pre-baked OS images per role. Deploy by flashing/PXE-booting. Interesting for hundreds of identical gNBs with standardized hardware. Heavy infrastructure requirement, complex upgrade story.

**6. Bundle Layers** -- OCI-style layered bundles. Shared base layer + role-specific deltas. Optimal transfer efficiency and natural caching. But significant rework of bundle format, builder, and launcher. Worthwhile only if bundle sizes become a real bottleneck.

**7. Pull-Based Agent + Depot** -- Management node runs an HTTP depot server. New nodes run a thin agent that pulls its role bundle and self-bootstraps. Scales naturally, pull-based (no SSH from management to workers). But converges with aether-ops' own planned node agent -- should probably BE the aether-ops agent rather than a separate bootstrap agent.

---

## Phased Roadmap

### Phase 1: Multi-role specs + new components
- Extract current `bundle.yaml` into `specs/management.yaml` (slim: no RKE2/Helm)
- Write `specs/sdcore.yaml` (RKE2 server, Helm, SD-Core charts, images)
- Write `specs/gnb.yaml` (Docker, gNB images, OS tuning)
- Add components: `docker`, `os_tuning`, `images`; add agent mode to `rke2`
- Builder already supports `--spec <dir>` for multi-spec builds
- **Enables:** manual multi-role deployment

### Phase 2: Depot staging
- Management spec gains `depot:` section
- New `depot` component stages sub-bundles + launcher to `/var/lib/aether-ops/depot/`
- Aether-ops gains "bootstrap node" workflow (push bundle, run launcher, monitor)
- **Enables:** automated multi-node provisioning from aether-ops UI

### Phase 3: Node agent integration (aether-ops work)
- Aether-ops node agent pulls its role bundle from depot on first contact
- Agent runs launcher locally, reports status back
- **Enables:** self-service provisioning at hundreds-of-gNBs scale

### Future: gNB migration to k8s
- When gNBs move from Docker to k8s, `specs/gnb.yaml` switches from `docker` to `rke2` (agent mode)
- RKE2 agent joins SD-Core's cluster using a join token
- Join token distribution becomes aether-ops' responsibility (passed via agent or API)

---

## Open Questions

1. **Single-node vs multi-node management spec:** Should there be one `management.yaml` that optionally includes RKE2 (for single-node all-in-one deployments), or two separate specs? The current bootstrap's single-node mode is useful for demos/testing.

2. **Join token flow:** When a gNB (future) or additional SD-Core node needs to join the cluster, where does the join token come from? Options: aether-ops API, depot metadata file, passed as env var to launcher.

3. **SD-Core HA:** Will there be multiple SD-Core nodes forming a k8s HA cluster? If so, the first SD-Core node is RKE2 server init, subsequent ones are RKE2 server join -- different configs from the same bundle.

4. **Depot format:** Flat filesystem directory (simple, SCP-friendly) vs HTTP server (enables pull-based agent)? Could start as filesystem and add HTTP later.
