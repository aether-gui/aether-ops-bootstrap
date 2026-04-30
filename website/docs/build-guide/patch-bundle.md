---
id: patch-bundle
title: Patching an existing bundle
sidebar_position: 9
---

# Patching an existing bundle

`cmd/patch-bundle` rewrites files inside an already-built `bundle.tar.zst`
without re-running `cmd/build-bundle`. This is the fast path for
operators who need to swap a few onramp template files (e.g. SDR
parameters, ZMQ ports, sd-core values) and don't want to wait for a full
rebuild that re-pulls images and APT packages.

For builds that need to be reproducible from the spec, prefer the
[`onramp.patches:`](./bundle-yaml-reference.md#onramp) block in
`specs/bundle.yaml` and a normal `make bundle`.

## Build it

```bash
make build-patch-bundle         # → dist/patch-bundle
```

## Inline overrides

The most common case: replace one or more files in the bundled
aether-onramp tree from local source files.

```bash
./dist/patch-bundle \
  --in  dist/bundle.tar.zst \
  --out dist/bundle-patched.tar.zst \
  --replace ocudu/roles/uEsimulator/templates/ue_zmq.conf=./ue_zmq.conf \
  --replace ocudu/roles/gNB/templates/gnb_zmq.yaml=./gnb_zmq.yaml \
  --replace ocudu/roles/gNB/templates/gnb_uhd_x310.yaml=./gnb_uhd_x310.yaml \
  --replace 5gc/roles/core/templates/sdcore-5g-values.yaml=./sdcore-5g-values.yaml
```

Each `--replace` is `<onramp-rel-path>=<local-file>`:

- The left-hand side is rooted at the bundled onramp tree (i.e. the path
  the file ends up at under `/var/lib/aether-ops/aether-onramp/` on the
  target host). Slashes only; no `..` segments.
- The right-hand side is a path on the build host. Both absolute and
  CWD-relative paths work.

`--replace` is repeatable. The target file must already exist in the
bundle — `patch-bundle` will not implicitly add files.

## Patches manifest

For larger sets, point `--patches` at a YAML file using the same schema
as `onramp.patches:` in `specs/bundle.yaml`:

```yaml
# patches.yaml
schema_version: 1
patches:
  - target: ocudu/roles/uEsimulator/templates/ue_zmq.conf
    source: ./ue_zmq.conf
  - target: ocudu/roles/gNB/templates/gnb_zmq.yaml
    content: |
      zmq_port: 5555
```

```bash
./dist/patch-bundle \
  --in  dist/bundle.tar.zst \
  --patches patches.yaml \
  --out dist/bundle-patched.tar.zst
```

`source:` paths are resolved relative to the patches file, not the CWD.

## Output and integrity

`patch-bundle`:

1. Extracts the input bundle to a temporary directory.
2. Applies the requested file replacements.
3. Recomputes per-file SHA256s for the onramp tree and updates the
   manifest, including `tree_sha256`.
4. Re-archives to `<out>.tmp` and renames atomically to `<out>` on success.
5. Writes a fresh `<out>.sha256` sidecar.

The manifest's `tree_sha256` change is what makes the launcher detect
the patched bundle as distinct from the unpatched one — without it, a
host already at the upstream onramp commit would skip re-extract on
the next `aether-ops-bootstrap upgrade`.

A patched bundle is functionally identical to one built fresh with the
same `onramp.patches:` block: identical `tree_sha256`, identical
per-file hashes, identical content on disk.

## Operational notes

- The tool refuses to overwrite the input file in place — pass a
  distinct `--out` (or `--output-dir`).
- The patched bundle is the same size as the input (file count is
  unchanged; only content varies). Wall time is dominated by tar+zstd
  of the full archive — for a multi-GB bundle, expect a few minutes.
- Failures leave no `--out` file behind: the staged output is removed
  if any step fails.
- `patches.yaml`'s `schema_version` is optional; if set, must be `1`.
- Re-patching a patched bundle works the same way; `tree_sha256`
  diverges again.

## Limitations

- Only the bundled onramp tree is patchable in v1. Patching helm-charts
  trees, debs, images, or other manifest components is not supported —
  let us know if you need it.
- Whole-file replace only. Partial edits via diff/regex are out of
  scope; they introduce brittleness against upstream onramp drift.
