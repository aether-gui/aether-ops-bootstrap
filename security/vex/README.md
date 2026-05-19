# OpenVEX statements

`openvex.json` is the canonical OpenVEX 0.2.0 document for the
aether-ops-bootstrap project. It feeds the `grype --vex` flag in
`launcher.yml`, `release.yaml`, and `distribute.yml`, and a copy is
published per-release alongside the SBOM and Grype scan output.

Statements are added when a CVE Grype surfaces is **not_affected** (we
do not exercise the vulnerable path) or **fixed** (the upstream patch
is rolled into the bundle but our pinned SBOM still names the
pre-fix version). Authoring these by hand is error-prone; use
[`vexctl`](https://github.com/openvex/vexctl) instead.

## Add a not_affected statement

```sh
vexctl create \
  --product "pkg:generic/aether-ops-bootstrap" \
  --vuln "CVE-2025-12345" \
  --status not_affected \
  --justification vulnerable_code_not_in_execute_path \
  --file security/vex/openvex.json
```

Repeat for each affected product line. `pkg:generic/aether-ops-bundle`
covers the offline bundle; the launcher uses
`pkg:generic/aether-ops-bootstrap`. Match what Grype reports in the
`artifact.purl` field of its JSON output.

## Validate locally

```sh
grype sbom:dist/sbom-bundle-<version>.spdx.json \
  --vex security/vex/openvex.json \
  -o table
```

CVEs covered by a `not_affected` / `fixed` statement drop off the
table view but remain in the SBOM so downstream consumers can audit
them independently.

## Notes

- Bump the top-level `version` field on every meaningful change. Grype
  ignores it but downstream tooling uses it to detect updates.
- Refresh `timestamp` when adding or modifying statements; this is the
  document's last-modified time, not per-statement metadata.
- The `@id` URL is the canonical public location of this file once a
  release is published. Updating the host or path means re-issuing the
  document with a new id.
