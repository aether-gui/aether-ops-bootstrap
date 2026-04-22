---
id: upgrades-and-repair
title: Upgrades and repair
sidebar_position: 6
---

# Upgrades and repair

Three commands, one loop. `install`, `upgrade`, and `repair` all walk the
component list in order — they differ only in the preconditions they check
and what they do with the state file.

## When to use which

| Situation | Command |
|---|---|
| Clean Ubuntu host, first time | `install` |
| Accidentally ran install twice (no state yet) | `install` |
| Re-run install on a host with existing state | `install --force` (rare; usually wrong) |
| Applying a newer bundle to an installed host | `upgrade` |
| A config was hand-edited or a file went missing | `repair` |
| Preview what any of the above would do | `check` |

When in doubt between `install --force`, `upgrade`, and `repair`:

- `install --force` — you want to *reset* the launcher's belief about this
  host to the passed bundle. Rarely correct.
- `upgrade` — you want *only the changes* between the installed bundle and
  the new bundle to apply. Almost always what you want when bundles differ.
- `repair` — you want *every component* re-applied using the passed bundle,
  regardless of state. Use when state says "installed" but disk says
  otherwise.

## `upgrade` in detail

```bash
sudo aether-ops-bootstrap upgrade --bundle bundle-2026.05.1.tar.zst
```

Steps:

1. **Preflight.** Same checks as `install`.
2. **Load state.** If no state file exists, the launcher starts from an empty
   state, just like a fresh install.
3. **Load the new manifest.** Parse the bundle; verify `schema_version`.
4. **Component loop.** For each component:
   - Compare current version (from state) to desired (from new manifest).
   - If they match, skip.
   - Otherwise, `Apply`.
5. **Write final state.** New bundle version, new bundle hash, updated
   per-component version. Append one `HistoryEntry` with `action: "upgrade"`.

`upgrade` is the **safe** command for applying a new bundle to a
healthy host. It touches only components whose desired versions changed.

### What upgrade doesn't do

- **Doesn't roll back.** 0.1.x has no "downgrade" concept; running upgrade
  with an older bundle is allowed but treated as a normal forward apply
  with older versions.
- **Doesn't restart a service whose version didn't change.** If
  `rke2.version` is the same in the old and new manifests, RKE2 is not
  restarted by `upgrade`.
- **Doesn't upgrade aether-ops' deployed workloads.** Those are aether-ops'
  responsibility, not the bootstrap's.

## `repair` in detail

```bash
sudo aether-ops-bootstrap repair --bundle bundle.tar.zst
```

Steps:

1. **Preflight.** Same checks.
2. **Load state.** If no state file exists, the launcher starts from an empty
   state.
3. **Load manifest.** Same as upgrade.
4. **Component loop.** For each component, `Apply` unconditionally using
   the manifest's desired values. No short-circuit on version match.
5. **Write final state.** History entry recorded with `action: "repair"`.

**Use repair when:**

- A sshd drop-in was hand-edited and you want it back to the bundled version.
- Someone `rm`'d a config file under `/etc/rancher/rke2/`.
- A systemd unit was modified and survives a restart but you want it
  restored.
- You suspect drift but don't know where.

**Don't use repair when:**

- You want to roll forward a version — that's `upgrade`.
- You want to wipe and redo — that's `install --force` after cleanup.

## `check` in detail

```bash
sudo aether-ops-bootstrap check --bundle bundle.tar.zst
```

Same component loop as `install` / `upgrade` / `repair`, but component
`Apply` methods are never called. Instead the per-component plans are
printed. In 0.1.x, `check` still writes state metadata and a history entry.

Output shape:

```
[debs] up to date (2026.04.1)
[ssh] up to date (2026.04.1)
[sudoers] up to date (2026.04.1)
[rke2] would apply (v1.33.1+rke2r1 -> v1.33.2+rke2r1, 9 actions)
  - write RKE2 config
  - extract RKE2 v1.33.2+rke2r1
...
```

Non-trivial uses for `check`:

- **Change management** — attach `check` output to a change ticket as
  evidence of exactly what a deploy would touch.
- **Change review** — run `check --bundle <new-bundle>` before an upgrade to
  see which components would apply. In 0.1.x, `check` does not detect
  template-only drift when the component version is unchanged.

## Pairing rules

A newer bundle and an older launcher (or vice versa) might or might not be
compatible; the safe envelope:

- Same `schema_version` in manifest and state = compatible.
- Different `schema_version` = preflight fails with a clear message.

The CI integration matrix tests:

- current launcher × current bundle
- current launcher × previous bundle
- previous launcher × current bundle

If a combination you need isn't one of those, test it in a VM before
applying to a production host.

## Recovery after a failed upgrade

A failed `upgrade` leaves:

- A state file with mixed versions (some components updated, the failing
  one did not).
- A diagnostic tarball in `/tmp`.
- A bootstrap log at `/var/lib/aether-ops-bootstrap/bootstrap.log`.

Recovery sequence:

1. **Inspect the log / diagnostic tarball** to understand what failed.
2. **Resolve the underlying issue** (disk space, firewall, whatever).
3. **Re-run `upgrade`.** The components that already updated will no-op;
   the one that failed will retry.
4. If the component is in a partially-applied state (half-written config,
   broken systemd unit), **run `repair`** instead of `upgrade` — repair
   re-applies regardless of state.
