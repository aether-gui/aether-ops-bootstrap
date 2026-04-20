---
id: index
title: Getting started
slug: /getting-started
sidebar_position: 1
---

# Getting started

This section gets you from **"I have a fresh Ubuntu Server, a launcher binary,
and a bundle tarball"** to **"aether-ops is running on it"** in one sitting.

If you want to *build* the launcher and bundle yourself, go to the
[Build Guide](/build-guide). This page is for operators who were handed the
finished artifacts.

## Prerequisites

### On the target host

- **Ubuntu Server**, version 22.04, 24.04, or 26.04. Desktop editions should
  work but are not tested.
- **amd64 architecture.** arm64 support is tracked but not in 0.1.x.
- **Root or sudo.** The launcher must run as root or via `sudo`.
- **systemd.** Present on every supported Ubuntu release by default.
- **At least 8 GB RAM and 40 GB free disk.** RKE2 plus the image store is
  the biggest consumer.
- **No prior bootstrap.** A prior successful install blocks `install`; use
  `upgrade` or `repair` (or `--force`) if you're intentionally re-running.
- **No internet required.** The host can be fully airgapped.

### What you should have in hand

Three files, typically handed to you together:

| File | Description |
|---|---|
| `aether-ops-bootstrap` | The launcher binary (executable). ~20 MB. |
| `bundle.tar.zst` | The offline payload. ~1 – 2 GB depending on variants. |
| `bundle.tar.zst.sha256` | Integrity check sidecar. Tiny. |

The `.sha256` sidecar is a standard one-line file:

```
<64-char-hex-hash>  bundle.tar.zst
```

## The two-command install

If your artifacts are already on the host, bootstrap reduces to:

```bash
sha256sum -c bundle.tar.zst.sha256
sudo ./aether-ops-bootstrap install --bundle bundle.tar.zst
```

That's it. See [your first bootstrap](./first-bootstrap.md) for the
step-by-step walkthrough with example output and the exact commands to run
before and after.

## What comes next

- **[Your first bootstrap](./first-bootstrap.md)** — copy, verify, install,
  observe. The commands you'll actually type.
- **[Verifying the install](./verifying.md)** — "what does success look like?"
- **[Common problems](./common-problems.md)** — the two or three things that
  trip up new operators and how to recover.
- **[Next steps](./next-steps.md)** — where to go after the bootstrap exits
  cleanly.

:::tip You don't need Kubernetes expertise to operate the install.
The launcher owns RKE2 entirely. You will not `kubectl apply` anything to
bootstrap the management plane. If something goes wrong, reach for the
launcher's `check` / `repair` / `diagnose` commands before reaching for
`kubectl`.
:::
