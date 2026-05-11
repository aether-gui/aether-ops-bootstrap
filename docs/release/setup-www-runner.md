# Set up the self-hosted GitHub runner in the `www` container

One-time setup so `.github/workflows/distribute.yml` can build, publish,
and open release PRs without an operator running the
`deploy-dist-site.md` procedure by hand.

The runner lives inside the LXD `www` container so the bundle's
`.deb` staging plus the ~3.4 GB publish step are local filesystem
operations against `/var/www/tools.jointpathfinding.com/...`
instead of multi-minute LXD HTTPS API transfers.

The runner is registered **org-wide** (under `aether-gui`) so other
repositories in the org can opt into it by adding
`runs-on: [self-hosted, bundle-dist]`. The label gates which jobs
land here.

## Prerequisites on the `www` container

The container needs Go, `make`, `rsync`, `git`, `jq`, `curl`, and
`gh` available on `PATH`. Most are already there; install whatever
isn't:

```bash
/snap/bin/lxc exec datacenter:www -- bash -c '
  apt-get update
  apt-get install -y curl jq rsync git make
  # GitHub CLI
  curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
    -o /usr/share/keyrings/githubcli-archive-keyring.gpg
  echo "deb [signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] \
        https://cli.github.com/packages stable main" \
    > /etc/apt/sources.list.d/github-cli.list
  apt-get update
  apt-get install -y gh
'
```

Go isn't packaged on the container's Ubuntu release at a version the
workflow accepts (it pins `go-version: 1.23`). `actions/setup-go@v5`
downloads its own toolchain, so no host install is required — but
the runner does need to be able to extract a tarball, so verify
`tar` is present.

## Register the runner

1. Visit https://github.com/organizations/aether-gui/settings/actions/runners/new
   (org-level Actions settings, "New self-hosted runner") and copy
   the registration token (single-use, expires in an hour).
   *Do not commit the token anywhere.*

2. Drop the runner into the container under `/opt/actions-runner`:

   ```bash
   /snap/bin/lxc exec datacenter:www -- bash -c '
     # As a service account: create a low-privilege user that owns
     # the runner directory plus has the supplementary group memberships
     # needed to write under /var/www.
     id aether-runner >/dev/null 2>&1 || useradd -m -s /bin/bash aether-runner
     usermod -aG www-data aether-runner
     install -d -m 750 -o aether-runner -g aether-runner /opt/actions-runner
   '
   ```

3. Download and configure the runner. Substitute `<REGISTRATION_TOKEN>`
   with the token from step 1. Note the org URL — without the
   repo path — so the runner is registered against the org and
   shared across `aether-gui/*`:

   ```bash
   /snap/bin/lxc exec datacenter:www -- bash -c '
     RUNNER_VERSION=$(curl -fsSL https://api.github.com/repos/actions/runner/releases/latest | jq -r .tag_name | sed s/^v//)
     cd /opt/actions-runner
     sudo -u aether-runner curl -fsSL -o actions-runner.tar.gz \
       "https://github.com/actions/runner/releases/download/v${RUNNER_VERSION}/actions-runner-linux-x64-${RUNNER_VERSION}.tar.gz"
     sudo -u aether-runner tar xzf actions-runner.tar.gz
     rm -f actions-runner.tar.gz
   '
   /snap/bin/lxc exec datacenter:www -- sudo -u aether-runner bash -c '
     cd /opt/actions-runner
     ./config.sh \
       --url https://github.com/aether-gui \
       --token <REGISTRATION_TOKEN> \
       --name www-aether \
       --labels bundle-dist \
       --work _work \
       --unattended \
       --replace
   '
   ```

4. Install the runner as a systemd service so it survives container
   restarts:

   ```bash
   /snap/bin/lxc exec datacenter:www -- bash -c '
     cd /opt/actions-runner
     ./svc.sh install aether-runner
     ./svc.sh start
     ./svc.sh status
   '
   ```

   Status should report `active (running)`.

## Grant write access to the publish target

The workflow runs `install -d -m 755 -o www-data -g www-data ...`
inside `$WWW_ROOT` and then `rsync -a --chown=www-data:www-data ...`.
`aether-runner` needs root for those two argv shapes but nothing
else, so a narrow sudoers drop-in is the right granularity:

```bash
/snap/bin/lxc exec datacenter:www -- bash -c '
  cat >/etc/sudoers.d/aether-runner <<"EOF"
# Allow the self-hosted runner to publish to /var/www without granting
# full root. The three commands are exactly what
# .github/workflows/distribute.yml needs:
#   - install -d ...           : creates the per-version directory
#   - rsync -a --chown=... ... : copies the rendered site in
#   - rm -rf <version-dir>     : prunes retired release directories
# The rm rule is tightly scoped to the publish tree so a typo can't
# wander outside aether-ops-bootstrap/.
aether-runner ALL=(root) NOPASSWD: /usr/bin/install -d -m 755 -o www-data -g www-data *
aether-runner ALL=(root) NOPASSWD: /usr/bin/rsync -a --chown=www-data\:www-data *
aether-runner ALL=(root) NOPASSWD: /usr/bin/rm -rf -- /var/www/tools.jointpathfinding.com/aether-ops-bootstrap/bootstrap/*
aether-runner ALL=(root) NOPASSWD: /usr/bin/rm -rf -- /var/www/tools.jointpathfinding.com/aether-ops-bootstrap/bundles/*
aether-runner ALL=(root) NOPASSWD: /usr/bin/rm -rf -- /var/www/tools.jointpathfinding.com/aether-ops-bootstrap/tools/*
EOF
  chmod 0440 /etc/sudoers.d/aether-runner
  visudo -c -f /etc/sudoers.d/aether-runner
'
```

The Distribute workflow already invokes both commands via `sudo`, so
no further change is needed once the drop-in lands.

## Verify the runner is healthy

From the org's *Settings → Actions → Runners* page on GitHub, the
new runner should appear as `www-aether` with status *Idle* and the
`bundle-dist` label. Kick off the `Distribute` workflow via *Run
workflow* with no inputs (it will default to today's UTC date with
`N=1`) — or tag the repo with `git tag vYYYY.MM.DD.N && git push
--tags` — and watch the run complete.

If the workflow fails on `Capture prior current-release SHAs`, the
runner doesn't have read access to `$WWW_ROOT/metadata.json` — add
`aether-runner` to `www-data` so it can read the tree:

```bash
/snap/bin/lxc exec datacenter:www -- usermod -aG www-data aether-runner
/snap/bin/lxc exec datacenter:www -- /opt/actions-runner/svc.sh restart
```

## Notes / gotchas

- The runner needs egress for `archive.ubuntu.com`, `github.com`,
  `ghcr.io`, `quay.io`, and `docker.io` so the bundle build can
  fetch indexes, RKE2, helm charts, and the 30 container images.
  If the container's egress is locked down, allow these explicitly
  or proxy them through an internal caching mirror.
- The bundle build pulls ~3.4 GB into `dist/`. The runner's `_work`
  directory needs at least that much free space plus headroom for
  the rendered site (`dist/release-site/`).
- `actions/setup-go@v5` caches the Go toolchain under
  `$RUNNER_TOOL_CACHE` (default `/opt/hostedtoolcache`); the runner
  owner needs write access to that path.
- The workflow opens a PR but does not auto-merge. Fill in the
  three `release_summary:` headlines and replace the placeholder
  `release_notes:` bullet on each artifact, then merge through
  `dev → main` per the usual merge-train workflow.
