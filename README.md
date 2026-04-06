# aether-ops-bootstrap

`aether-ops-bootstrap` is the bootstrap and packaging system for self-contained Aether 5G deployments, with an initial focus on running on top of an existing Ubuntu host and a future path toward full ISO-based installation.

The primary goal is to produce a **single distributable artifact** that can be delivered into an air-gapped environment and used to bring up the baseline software and assets required to launch the Aether operations experience.

This repository is intended to be the **source of truth** for how those bootstrap artifacts are built, what they contain, and how they are installed.

## Goals

* Produce a **single-file bootstrap artifact** for air-gapped deployment.
* Keep builds **deterministic and reproducible**.
* Pin all external source dependencies, including OnRamp repositories and submodules.
* Bundle all required payload assets so runtime installation does not depend on internet access.
* Make installation **safe to re-run** and **idempotent by design**.
* Preserve a clean architectural path from:

  * bootstrap-on-existing-OS
  * to future ISO-based full system installation
* Keep the bootstrap logic modular so installation behavior can be assembled from ordered install modules.

## Current Direction

The current working design is:

* A **single static Go bootstrap binary** is the user-facing artifact.
* A `.tar.gz` **payload archive** is appended to the end of that binary.
* The Go bootstrap reads its own executable file, locates the appended payload via footer metadata, and extracts the payload using Go code rather than relying on host tools such as `tar`.
* The payload contains the pinned repositories, submodules, image tarballs, documentation, manifests, and other required installation assets.

Conceptually:

```text
[bootstrap binary][payload.tar.gz][footer metadata]
```

This approach is intended to reduce reliance on the host environment while still keeping the solution practical to build, inspect, and evolve.

## Why This Exists

Aether deployments need to operate in environments where external network access may not be available. That means the bootstrap process must be able to:

* carry all required artifacts with it
* validate the target system locally
* extract and install without downloading dependencies at runtime
* provide enough local documentation and diagnostics to support offline operation

This repository exists to define and automate that process.

## Scope

### In Scope

* Packaging a single distributable bootstrap artifact
* Bundling required assets for offline installation
* Pinning and packaging raw repositories and submodules
* Packaging image tarballs for registry import / installation workflows
* Running bootstrap/install logic on supported Ubuntu hosts
* Defining modular installation phases and ordered install modules
* CI/CD automation for reproducible bundle generation
* Offline-friendly documentation and diagnostics requirements

### Out of Scope

* The full Aether operations web application itself
* Ongoing runtime management after bootstrap is complete
* Final ISO implementation details
* Long-term update/delta packaging design

## Supported Target Assumptions

The current intended baseline assumptions are:

* **OS family:** Ubuntu
* **OS version:** 22.04 and newer
* **Architecture:** x86_64 only
* **Privileges:** root required at installer launch

Additional constraints such as minimum disk space, minimum memory, and other environment checks will be defined as testing progresses. The bootstrap should provide explicit validation logic for these values rather than burying them in scattered scripts.

## High-Level Architecture

### 1. Bootstrap Binary

The Go bootstrap binary is responsible for:

* locating and reading the appended payload
* validating target system requirements
* extracting the payload to a staging location
* verifying bundle metadata and checksums
* orchestrating ordered installation modules
* reporting detailed errors on failure
* supporting safe re-runs

### 2. Appended Payload

The payload is currently planned to be a `.tar.gz` archive appended to the binary. It will contain the assets needed for bootstrap and installation.

Expected contents include:

* pinned OnRamp repository snapshot(s)
* pinned submodules
* container image tarballs
* helper binaries and install assets
* manifests and checksums
* bundled documentation

### 3. Footer Metadata

A small footer appended to the end of the combined file will allow the Go bootstrap to determine:

* payload offset
* payload size
* format/version metadata
* integrity information needed by the bootstrap

The exact footer schema still needs to be finalized.

### 4. Modular Install Architecture

The bootstrap should not hardcode all installation behavior into one linear block of logic.

Instead, the internal Go architecture should support **ordered install modules** that can operate on the extracted payload and target system. These modules may perform actions such as:

* validate prerequisites
* install required packages or bundled binaries
* move files into place
* edit configuration files
* load image tarballs
* run commands or setup procedures
* invoke bundled tooling and installers

This modular model should allow developers to define installation behavior in a structured and extensible way while still preserving a clear default flow.

## Initial Installer Behavior

The current intended default flow is:

1. Display help / entry options
2. Validate host assumptions
3. Extract the appended payload
4. Install prerequisites and bundled components
5. Run setup procedures and installers via ordered install modules
6. Exit cleanly with success or a detailed error

Initial behavioral requirements:

* installation should be **idempotent by design**
* it should be **safe to re-run**
* partial failure should produce a **detailed error and exit**
* explicit resume support is **not required initially**

## Source and Dependency Model

This repository will need to package external sources in a reproducible way.

Current direction:

* package **raw repositories**
* package **raw submodules**
* pin them to specific **commits / hashes / tags**

This is especially important for OnRamp, since it is both repository-driven and submodule-driven. The bootstrap bundle must reflect an exact known-good source state rather than depending on mutable upstream branches.

## Image Packaging Model

The current direction is to package container images as **image tarballs**.

Reasons:

* simple to produce in CI
* easy to inspect
* easy to import or push into Harbor later
* avoids introducing unnecessary complexity too early

The exact image inventory and manifest structure still need to be defined.

## Integrity Model

Initial integrity protection will be hash-based only.

Current requirement:

* generate and store checksums for bundled assets and/or payload contents

Signing may be added later, but is not part of the current initial design.

## CI/CD and Repository-Driven Builds

This repository is intended to be **repo-driven**.

The expected model is:

* all build inputs are version-controlled
* GitHub Actions performs packaging
* changes to the repository produce a new deterministic bundle artifact
* external repositories and submodules are resolved using pinned references

The exact structure of the build input files still needs to be defined, but this repository should eventually contain the canonical definitions for:

* source repositories and refs
* image inventories
* bundle metadata
* install behavior / module composition
* documentation assets

## Future ISO Relationship

The long-term plan includes a custom OS / distribution ISO.

The intended architectural boundary is:

* **ISO owns OS installation**
* **bootstrap owns platform and application bring-up**

The bootstrap should therefore be designed so it can be reused inside a future ISO-based workflow rather than being thrown away when ISO support is introduced.

## Requirements Still to Be Finalized

The following areas are still open and need to be designed in more detail:

### Bundle Metadata / Manifest Schema

Need to define what the bundle declares, such as:

* bundle version
* bootstrap version
* build timestamp
* supported OS/arch
* source inventory
* image inventory
* file inventory
* checksums
* enabled install modules / default flow

### Footer Schema

Need to define the exact footer layout used to locate and validate the payload.

### Staging / Install Paths

Need to define where extraction, logs, runtime assets, and support data should live on disk.

### Minimum System Requirements

Need tested values for:

* disk space
* memory
* any other environmental assumptions

### Logging and Support Bundles

Need a concrete design for:

* what logs are captured
* where logs are stored
* how offline support bundles are generated
* what diagnostics are included

### Install Module Model

Need to define the shape of the modular installer architecture in Go, including how ordered install modules are registered, configured, and executed.

### Build Input Layout

Need to define the repository structure and source-of-truth files that drive packaging.

## Suggested Near-Term Focus

The next design steps should focus on the pieces that anchor the format and the code structure:

1. Define the bundle manifest schema
2. Define the footer schema
3. Define the Go install module architecture
4. Define the repository layout and build inputs
5. Define logging/support bundle requirements

## Status

This README reflects the **initial architecture direction** and current assumptions. It is intentionally focused on capturing what is known now, where the design is headed, and what still needs to be resolved next.

As implementation begins, this document should evolve from a concept overview into a more authoritative description of the actual bundle format, module architecture, and build process.
