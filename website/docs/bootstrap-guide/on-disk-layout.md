---
id: on-disk-layout
title: On-disk layout
sidebar_position: 5
---

# On-disk layout

Every file the launcher reads or writes on the target host, by location.
Useful when:

- Reviewing what a bootstrap run will change.
- Auditing a host after install.
- Cleaning up a half-installed host.

## Everything the launcher owns

```
/usr/local/bin/
├── helm                                   # Helm binary
├── kubectl                                # symlink to RKE2's kubectl
└── aether-ops                             # aether-ops daemon binary

/var/lib/rancher/rke2/                     # RKE2 data directory
├── bin/kubectl, containerd, …             # RKE2 ships its own toolchain
└── agent/images/*.tar.zst                 # airgap images staged here

/etc/rancher/rke2/
├── config.yaml                            # written by launcher
└── rke2.yaml                              # kubeconfig, mode 0640, group aether-ops

/etc/systemd/system/
├── rke2-server.service                    # from RKE2 tarball
└── aether-ops.service                     # from bundle

/etc/aether-ops/
└── (created by launcher; runtime config may be added later)

/var/lib/aether-ops/
├── aether-onramp/                         # cloned at build, staged here
└── helm-charts/<name>/                    # per helm_charts: entry

/etc/ssh/sshd_config.d/
└── 01-aether-password-auth.conf           # Match User <onramp>

/etc/sudoers.d/
└── <onramp_user>                          # NOPASSWD: ALL

/etc/profile.d/
└── rke2.sh                                # PATH + KUBECONFIG for interactive shells

/var/lib/aether-ops-bootstrap/
├── state.json                             # state file (schema_version 1)
└── bootstrap.log                          # tee'd log from every run
```

## Users and groups created

| Name | Type | Purpose |
|---|---|---|
| `aether-ops` | system user + group | Service account running the aether-ops daemon. In the `aether-ops` group (which can read the kubeconfig). |
| `aether` (default) | human user + group | Onramp user for Ansible SSH. `NOPASSWD: ALL`. Password auth enabled only for this user. |

The onramp user's name is configurable via `aether_ops.onramp_user` in
`bundle.yaml`.

## Network listeners the launcher *doesn't* own but expects

- `:22` — sshd. Must be running and accept connections for the onramp
  user. The launcher writes an `sshd_config.d/` drop-in and restarts `ssh`
  or `sshd`.
- `:6443` — RKE2 Kubernetes API, after the `rke2` component finishes.
- `:8186` — aether-ops HTTP API and health endpoint in 0.1.x.

## Files the launcher reads but does not write

- `/etc/os-release` — parsed to determine Ubuntu suite.
- `/run/systemd/system` — stat'd to confirm systemd is PID 1.
- The bundle tarball (`--bundle <path>`) — extracted to a temp directory.

## Cleanup — reversing a bootstrap

There's no official "uninstall" command in 0.1.x. If you need to wipe:

```bash
# Stop services
sudo systemctl stop aether-ops rke2-server
sudo systemctl disable aether-ops rke2-server

# Uninstall RKE2 (their official script)
sudo /usr/local/bin/rke2-uninstall.sh

# Remove launcher-owned files
sudo rm -rf \
  /etc/aether-ops \
  /var/lib/aether-ops \
  /var/lib/aether-ops-bootstrap \
  /usr/local/bin/aether-ops \
  /usr/local/bin/kubectl \
  /etc/ssh/sshd_config.d/01-aether-password-auth.conf \
  /etc/sudoers.d/aether \
  /etc/profile.d/rke2.sh

# Reload services
sudo systemctl daemon-reload
sudo systemctl restart ssh

# Optionally remove users
sudo userdel aether-ops
sudo userdel aether
```

**Do not** do this to a production node. It's listed here because cleanup
procedures are part of understanding the layout.

A proper `uninstall` command is planned — see
[roadmap](./roadmap.md).

## Extraction temp directory

At install time the launcher extracts the bundle under a temp directory
(typically beneath `/tmp`). The temp directory is removed when the launcher
returns, including failed runs.
