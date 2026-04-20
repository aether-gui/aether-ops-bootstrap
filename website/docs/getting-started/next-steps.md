---
id: next-steps
title: Next steps
sidebar_position: 5
---

# Next steps

The bootstrap's job is done. aether-ops is running. What now?

## Hand off to aether-ops

Everything beyond "the platform layer exists" belongs to aether-ops, not this
project. That includes:

- Adding more nodes (SD-Core, gNBs).
- Deploying 4G / 5G workloads onto RKE2.
- Managing SSH keys across the fleet.
- Day-2 operations — upgrades of the workloads, not of RKE2 itself.

Open the aether-ops UI (the bootstrap log printed its URL on success) and
continue in aether-ops' own documentation.

## Keep the artifacts

Keep the launcher binary and the bundle tarball on the node — a future
`upgrade` or `repair` will need them. Typical convention:

```bash
sudo mkdir -p /opt/aether/bootstrap
sudo mv aether-ops-bootstrap bundle.tar.zst bundle.tar.zst.sha256 /opt/aether/bootstrap/
```

They are read-only after install; you don't need them on the `$PATH`.

## Understand the launcher more deeply

- **[Bootstrap Guide](/bootstrap-guide)** — CLI reference, component details,
  state file schema, upgrade and repair semantics, troubleshooting.
- **[CLI reference](/bootstrap-guide/cli-reference)** — every subcommand and
  flag with worked examples.

## Multi-node (coming in 0.2+)

The 0.1.x line is single-node. You can experiment with the `--roles` flag
today (`mgmt`, `core`, `ran`) to exercise multi-node shapes, but the
production multi-node story — per-role bundles, depot staging, aether-ops
node agent driving provisioning — lands in 0.2 and later. See the
[roadmap](/bootstrap-guide/roadmap) for where this is going.

If you're evaluating the project *for* a multi-node deployment, the roadmap
page is the right thing to read next.

## Building your own bundle

If you're responsible for producing bundles (different `.deb` pins,
internally mirrored sources, a custom aether-ops build), jump to the
[Build Guide](/build-guide).
