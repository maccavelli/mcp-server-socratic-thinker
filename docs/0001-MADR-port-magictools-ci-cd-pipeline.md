---
status: proposed
date: 2026-08-29
decision-makers: Socratic Thinker maintainers
consulted: mcp-server-magictools CI pipeline as reference implementation
informed: Socratic Thinker contributors and release consumers
---

# Port the hardened magictools CI/CD pipeline to mcp-server-socratic-thinker

## Context and Problem Statement

`mcp-server-socratic-thinker` currently ships its release artifacts from a
minimal three-job workflow at
`.github/workflows/ci.yml` (72 lines):

```yaml
on:
  push:
    branches: [main]
    tags: ['v*']
  pull_request:

jobs:
  validate:        # ubuntu-latest: go mod download, go test, go vet, golangci-lint v2.13.1
  build:           # needs validate, ubuntu-latest: make build-all, SHA256SUMS, upload artifact
  release:         # needs build, tag-only, ubuntu-latest: softprops/action-gh-release@v3
```

That workflow works for "Linux-only, no native smoke, no installer" but it
has accumulated concrete weaknesses that the fleet's reference project,
`mcp-server-magictools`, has already solved. The magictools pipeline at
`mcp-server-magictools/.github/workflows/ci.yml` (342 lines, last touched
in commit `bcd1ac5` "ci: harden releases and add binary-only installers")
represents the converged target shape. Adopting the same pattern in
socratic-thinker is the natural next step; the Makefile already mirrors
the same `build-all` / `linux` / `darwin-arm64` / `windows-amd64` targets
(`Makefile:19-31`), the `cmd/mcp-server-socratic-thinker/version.go` file
already exposes `RawVersion` and `Version` (line 10-11) the way the magictools
binary is stamped (`cmd/mcp-server-magictools/version.go:7-11`), and both
repos use an identical pinned `golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.1`
in their respective workflows and lint invocations.

The known gaps in the current socratic-thinker pipeline are:

* **Trigger surface**: only `main` and `v*` tags; no `workflow_dispatch`,
  no run-on-all-branches for feature-branch CI feedback.
* **No concurrency control**: rapid pushes to a PR queue up duplicate
  runners, wasting minutes and producing inconsistent intermediate results.
* **No Node.js runtime pinning**: JavaScript-based Actions run on the
  runner default. If that default shifts, behavior shifts silently.
* **Action references are floating tags** (`actions/checkout@v7`,
  `actions/setup-go@v7`, `actions/upload-artifact@v7`,
  `actions/download-artifact@v8`, `softprops/action-gh-release@v3`),
  not commit SHAs with version comments. Tag references can be retargeted
  upstream; the magictools workflow pins every action to a SHA with a
  `# vX.Y.Z` comment.
* **No `gofmt -l` drift gate**: an unformatted file can land and only
  surface when a downstream maintainer runs `make fmt`.
* **No `go mod tidy` cleanliness gate**: dirty `go.mod`/`go.sum` can
  pass CI today and break a downstream consumer's build.
* **`go test` runs with the default `CGO_ENABLED`**: cgo can be enabled
  on a developer's machine and pass there while failing on Linux runners
  that lack the toolchain, or vice versa. The magictools pipeline
  enforces `CGO_ENABLED=0` for the test step explicitly.
* **No native macOS/Windows builds or tests**: the published
  `mcp-server-socratic-thinker-darwin-arm64` and `…-windows-amd64.exe`
  artifacts are built only by cross-compile on Linux; the binaries are
  never executed on their target OS before publication.
* **No tag-format validation**: a tag like `v1.2.3-rc1` would still
  trigger a release because the trigger pattern is a bare `v*` glob.
* **No post-build binary verification**: there is no check that the
  produced binary actually reports `--version` matching the tag, and
  no check that the binary was compiled with `CGO_ENABLED=0` (the
  Makefile sets the flag, but CI does not assert it survived into the
  artifact).
* **No installer scripts**: the release ships only the binaries. The
  magictools project added `scripts/install.sh`, `scripts/install.ps1`,
  and `scripts/install_test.sh` in the same hardening commit so a
  user can `curl -fsSL …/install.sh | sh` or
  `irm …/install.ps1 | iex` to bootstrap. The `install.sh` is also
  shellchecked and exercised offline by `install_test.sh` inside CI.
* **No SHA256SUMS-versioned manifest**: the existing pipeline writes a
  single `SHA256SUMS` (computed in the `build` job at
  `ci.yml:36-53`). The magictools pipeline additionally produces
  `SHA256SUMS-$VER` in the `release` job and uploads both, so consumers
  pinning a specific version can verify against the matching manifest
  without re-downloading `SHA256SUMS`.
* **Release job is a third-party action**: `softprops/action-gh-release@v3`
  uploads whatever sits in `dist/*` verbatim. There is no pre-upload
  re-verification of cgo-freeness, no `--version` stamping check, no
  alias-stamping (versioned + non-versioned filenames coexisting), and
  no re-check against `SHA256SUMS`.
* **No timeouts**: any of the three jobs can hang indefinitely.

How should `mcp-server-socratic-thinker`'s CI/CD pipeline be structured
so that its release artifacts are built, verified on every published
target OS, and published with the same guarantees the magictools fleet
already provides?

## Decision Drivers

* Single workflow, jobs ordered by `needs`, covering quality, native
  build/test, native smoke, and release publication.
* Cgo-free binaries, verified at compile time and again at upload time.
* Tag-format safety (`vX.Y.Z` only) before any release machinery runs.
* Cross-platform confidence: every published OS/arch binary is
  actually executed on its native OS in CI before a release tag can
  publish.
* Reproducible checksum manifest, published alongside versioned assets
  with non-versioned aliases for `latest`-style consumption.
* Pinned action SHAs with `vX.Y.Z` comments, pinned Node.js runtime
  via `FORCE_JAVASCRIPT_ACTIONS_TO_NODE24`, pinned Go toolchain via
  `go-version-file: go.mod`.
* Concurrency control so PR/branch pushes supersede but tag pushes do
  not get cancelled mid-upload.
* First-class installer scripts (POSIX + PowerShell) tested in CI,
  shipped as release assets.
* Existing release consumers must continue to find assets at the same
  paths; new paths are additive only.

## Considered Options

* Keep the current minimal three-job Linux-only workflow.
* Port the magictools pipeline shape, adapted to socratic-thinker.
* Replace the workflow with a release-orchestration tool such as
  `goreleaser` or `go-github-release`.

## Decision Outcome

Chosen option: "Port the magictools pipeline shape, adapted to
socratic-thinker", because the magictools pipeline already encodes the
fleet's converged answer to every driver above, the socratic-thinker
Makefile and `version.go` already match the magictools shape, and
adopting the same workflow preserves a single mental model across the
fleet without adding a new release-orchestration tool.

The adopted pipeline has four jobs in one workflow file at
`.github/workflows/ci.yml`:

1. **`go`** (`ubuntu-24.04`, `timeout-minutes: 25`). Runs the full
   quality gate (Node 24 assertion, `gofmt -l` drift, `go mod tidy`
   cleanliness, `go vet ./...`, `CGO_ENABLED=0 go test ./...`,
   `golangci-lint v2.13.1` via `make lint`) on every push, PR, and
   tag. On tags, validates the tag against `^v[0-9]+\.[0-9]+\.[0-9]+$`,
   runs `make build-all VERSION=$TAG`, asserts the linux/amd64 binary
   reports the expected `--version`, asserts every artifact carries
   `build CGO_ENABLED=0`, and writes `SHA256SUMS` into `dist/`. Uploads
   `dist/` as `mcp-server-socratic-thinker-$VERSION`.
2. **`go-native`** (matrix `macos-15` and `windows-2025`,
   `fail-fast: false`, `timeout-minutes: 30`). On every push/PR/tag,
   builds, vets, and tests on each native runner with
   `CGO_ENABLED: "0"`. On Windows, additionally runs
   `scripts/install.ps1 -WhatIf` and asserts the output reports the
   `Programs\socratic-thinker` install directory and contains no
   `configure`/`service install` text.
3. **`smoke-native`** (tag only; `needs: [go]`; matrix `macos-15` and
   `windows-2025`; `timeout-minutes: 15`). Downloads the artifact
   produced by `go`, executes the macOS and Windows binaries with
   `--version`, and asserts the reported version equals the tag.
4. **`release`** (tag only; `needs: [go, go-native, smoke-native]`;
   `ubuntu-24.04`; `timeout-minutes: 15`; `permissions: contents: write`).
   Downloads the artifact, re-runs `sha256sum -c SHA256SUMS`, re-asserts
   every binary is cgo-free, re-asserts the linux/amd64 `--version`,
   stages versioned (`-$VER` suffix) and non-versioned alias copies,
   writes `SHA256SUMS-$VER` plus `SHA256SUMS`, copies
   `scripts/install.sh` and `scripts/install.ps1` into the asset set,
   creates the GitHub Release with auto-generated notes if missing, and
   uploads all assets with `gh release upload … --clobber`.

Cross-cutting settings applied at the workflow level:

* `on.push.branches: ['**']`, `on.push.tags: ['v*']`, plus
  `pull_request` and `workflow_dispatch` so the pipeline is reusable
  for ad-hoc re-runs.
* `concurrency.group: ci-${{ github.workflow }}-${{ github.ref }}` with
  `cancel-in-progress: ${{ github.ref_type != 'tag' }}` so tag runs
  are never cancelled during upload.
* `env.FORCE_JAVASCRIPT_ACTIONS_TO_NODE24: true` and an explicit Node
  major-version assertion step.
* `permissions.contents: read` at the workflow level; `contents: write`
  is granted only to the `release` job.
* All `uses:` references are pinned to commit SHAs with `vX.Y.Z`
  comments, matching the magictools convention.

The current minimal `ci.yml` is deleted; the new pipeline replaces it
in place.

### Consequences

* Good, because every published OS/arch binary is built, tested, and
  smoke-run on its target OS before a tag can publish.
* Good, because `CGO_ENABLED=0` is asserted at compile, test, and
  upload time, closing the silent-cgo class of failure.
* Good, because format drift (gofmt, `go mod tidy`) is caught in CI
  rather than by a downstream maintainer.
* Good, because release assets are reproducible from
  `SHA256SUMS-$VER` plus alias files, supporting both pinned-version
  and `latest`-style consumers.
* Good, because shell- and PowerShell-based installers ship as
  first-class release assets, exercised by CI before publication.
* Good, because action references are SHA-pinned; a malicious or
  accidental upstream re-tag of an `@vN` cannot change behavior
  silently.
* Bad, because workflow wall-clock cost rises: a tag push now runs
  four jobs across three operating systems plus the existing Linux
  job, replacing the prior single-Linux set.
* Bad, because `workflow_dispatch` and `branches: ['**']` increase
  the number of CI runs; branch-protection rules must remain the
  source of truth for required checks.
* Bad, because the magictools `installer` jobs assume the installer
  scripts exist; the plan must create `scripts/install.sh`,
  `scripts/install.ps1`, and `scripts/install_test.sh` from scratch
  with socratic-thinker-specific names and install-dir conventions.
* Bad, because the `gh release upload --clobber` path mutates any
  existing release; tag immutability discipline becomes a release
  prerequisite.
* Bad, because pinning actions to SHAs means routine Action upgrades
  require a workflow PR (intentional friction; flagged here so the
  cost is acknowledged).

### Confirmation

The decision is implemented when:

* A `v0.0.1`-style tag (after a sanity build) produces a GitHub Release
  with the following assets and the workflow logs show every assertion
  step passing: `mcp-server-socratic-thinker-linux-amd64-<vX.Y.Z>`,
  `mcp-server-socratic-thinker-linux-amd64` (alias),
  `mcp-server-socratic-thinker-darwin-arm64-<vX.Y.Z>`,
  `mcp-server-socratic-thinker-darwin-arm64`,
  `mcp-server-socratic-thinker-windows-amd64-<vX.Y.Z>.exe`,
  `mcp-server-socratic-thinker-windows-amd64.exe`,
  `SHA256SUMS-<vX.Y.Z>`, `SHA256SUMS`, `install.sh`, `install.ps1`.
* Running the workflow on a feature branch (any non-`main` ref) shows
  the `go` and `go-native` jobs pass; the `release` and `smoke-native`
  jobs are skipped.
* Pushing a tag whose name fails `^v[0-9]+\.[0-9]+\.[0-9]+$` (e.g.
  `v1.2.3-rc1`) causes the `go` job's tag-validation step to fail
  before any artifact is produced.
* `make build-all` on Linux followed by
  `go version -m dist/mcp-server-socratic-thinker-linux-amd64 | grep
  'build\tCGO_ENABLED=0'` returns one match per artifact.
* `sh scripts/install_test.sh` exits `0` on Linux after a fresh
  checkout.
* Re-running the same tag push (idempotency check) does not error:
  `gh release create` is a no-op when the release exists, and
  `gh release upload --clobber` overwrites cleanly.

## Pros and Cons of the Options

### Keep the current minimal three-job Linux-only workflow

* Good, because the diff is zero and no risk is introduced.
* Good, because Linux-only CI is cheap (one runner, one OS).
* Bad, because every weakness listed in Context remains: no native
  smoke, no installer assets, no gofmt/`go mod tidy` gate, no cgo
  assertion, no SHA256SUMS-$VER, no Node 24 pinning, no concurrency
  control, no timeouts, no PowerShell path.
* Bad, because the socratic-thinker release continues to publish
  macOS and Windows binaries that have never been executed in CI,
  so a regression that only manifests on those platforms (process
  forking, path separators, signal handling, line endings) reaches
  users untested.
* Bad, because the fleet drifts: magictools converges on a richer
  pipeline; socratic-thinker does not.

### Port the magictools pipeline shape, adapted to socratic-thinker

* Good, because the Makefile, `version.go`, `go.mod`,
  `.golangci.yml`, and `golangci-lint v2.13.1` pin already match
  the magictools shape, so the workflow body is largely a
  variable rename (`magictools` → `socratic-thinker`,
  `~/.local/bin` → `~/.local/bin`, `Programs\magictools` →
  `Programs\socratic-thinker`).
* Good, because every driver above is satisfied by an existing
  pattern; no novel orchestration design is required.
* Good, because the four-job split (`go`, `go-native`,
  `smoke-native`, `release`) cleanly separates concerns and lets
  required-check configuration in branch protection express the
  actual release gate as `release`.
* Good, because shipping `install.sh` and `install.ps1` brings the
  release story to parity with the rest of the fleet.
* Neutral, because the existing `softprops/action-gh-release@v3`
  third-party dependency is removed in favor of `gh release create`
  + `gh release upload --clobber`; a future maintainer who wants the
  convenience of the third-party action must re-add it deliberately.
* Bad, because the additional macOS/Windows runners materially
  increase CI minutes per push; the team should expect a
  multi-platform bill on every PR.
* Bad, because pinning actions to SHAs adds upgrade friction; this
  is by design but must be communicated.
* Bad, because the workflow file grows from 72 to roughly 340
  lines; reviewers need a fuller mental model.

### Replace the workflow with `goreleaser` or similar

* Good, because a dedicated tool produces a complete release
  (artifacts, checksums, archives, SBOM, Docker images, Homebrew
  formulae, etc.) from a single declarative config.
* Good, because `goreleaser`'s changelog generation and Homebrew tap
  integration are fleet-leader features.
* Bad, because socratic-thinker does not currently publish Homebrew
  formulae, Docker images, or archives; the tool's headline features
  are unused.
* Bad, because adopting `goreleaser` introduces a new binary
  release contract that diverges from the magictools fleet's `gh
  release`-based contract; fleet consistency is broken.
* Bad, because `goreleaser`'s cross-compile defaults assume
  `CGO_ENABLED=0`-safe code; without a separate native smoke job
  the same blind spot persists.
* Bad, because it is the largest of the three changes and the
  smallest of the three justifications.

## More Information

### Reference: magictools workflow anatomy

The chosen option is grounded in `mcp-server-magictools/.github/workflows/ci.yml`
(342 lines, commit `bcd1ac5`, 2026-08-29). Key excerpts, with their
socratic-thinker equivalents:

| Magictools (`mcp-server-magictools/.github/workflows/ci.yml`) | Socratic-thinker equivalent | Purpose |
|---|---|---|
| Lines 13-15 (`concurrency` block with `cancel-in-progress` guarded by `github.ref_type != 'tag'`) | New — adopt verbatim | Cancel superseded branch/PR runs; never cancel tag runs |
| Lines 18-20 (`FORCE_JAVASCRIPT_ACTIONS_TO_NODE24`, `NODE_VERSION: "24"`) plus lines 45-49 (`Assert Node.js major version`) | New — adopt verbatim | Pin JS-action runtime to Node 24 LTS |
| Lines 35 (`actions/checkout@3d3c42e5... # v7.0.1`), 40 (`actions/setup-node@820762786... # v7.0.0`), 52 (`actions/setup-go@b7ad1dad... # v7.0.0`), 160 (`actions/upload-artifact@043fb46d... # v7.0.1`), 229 (`actions/download-artifact@3e5f45b... # v8`) | Replace `actions/checkout@v7`, `actions/setup-go@v7`, `actions/upload-artifact@v7`, `actions/download-artifact@v8` in `.github/workflows/ci.yml` of socratic-thinker with SHA pins | Prevent silent upstream retag of action references |
| Lines 57-65 (`gofmt -l cmd internal`) | New | Drift gate; mirrors the implicit `make fmt` target already in `Makefile:39-40` |
| Lines 67-78 (`go mod tidy` clean check) | New | Drift gate; mirrors local developer hygiene |
| Lines 83-84 (`CGO_ENABLED=0 go test ./...`) | New — socratic-thinker currently runs `go test ./...` without the flag | Force cgo-free test execution |
| Lines 86-90 (golangci-lint install + `make lint`) | Already present in socratic-thinker `ci.yml:20-21`; keep | Single source of truth for lint invocation |
| Lines 92-104 (POSIX installer check: `sh -n`, last-line `main "$@"` check, shellcheck, `sh scripts/install_test.sh`) | New — requires adding `scripts/install.sh`, `scripts/install_test.sh` | CI-asserted installer correctness |
| Lines 106-156 (`Build release binaries` step with tag regex `^v[0-9]+\.[0-9]+\.[0-9]+$`, `--version` stamping check, cgo-free re-check, `SHA256SUMS` generation) | New | Release-time verification |
| Lines 167-212 (`go-native` matrix with `macos-15` and `windows-2025`, PowerShell `-WhatIf` dry-run + assertion text) | New — replace socratic-thinker's matrix-less `validate`/`build`/`release` shape | Native OS build + test + installer dry run |
| Lines 215-248 (`smoke-native` matrix downloading the artifact and asserting `--version`) | New | Tag-only native binary execution |
| Lines 251-342 (`release` job: re-checksum, re-cgo-check, re-`--version`, versioned + alias staging, `SHA256SUMS-$VER`, install scripts attached, `gh release create`/`upload --clobber`) | Replaces socratic-thinker's `softprops/action-gh-release@v3` invocation at `ci.yml:66-71` | Controlled, re-verified, asset-rich release |

### Socratic-thinker anchors that already match

* `Makefile:1-31` already declares `BINARY_NAME=mcp-server-socratic-thinker`,
  `linux`, `darwin-arm64`, `windows-amd64` targets, and the
  `-trimpath -tags netgo -ldflags "-X main.RawVersion=$(VERSION)"`
  flag set used by the magictools workflow to assert `--version`
  equality post-build.
* `cmd/mcp-server-socratic-thinker/version.go:10-11` already exposes
  `var RawVersion = "v4.4.4"` and `var Version = strings.TrimPrefix(RawVersion, "v")`
  exactly as the magictools binary does, so the CI `--version` stamp
  check is portable as written.
* `cmd/mcp-server-socratic-thinker/root.go:34` sets
  `rootCmd.Version = Version`, which prints via Cobra's `--version`
  flag.
* `.golangci.yml` already runs `golangci-lint v2.13.1` via `make lint`
  (Makefile:45-51) and matches the magictools lint configuration
  closely (same enabled linters, same `gocognit`/`gocyclo`
  thresholds; the only delta is a magictools-specific `gosec`
  exclusion for `G703` justified inline at `mcp-server-magictools/.golangci.yml:52-56`
  — socratic-thinker does not have the same datadir, so no analogous
  exclusion is needed).

### Files in scope for the paired plan

* `.github/workflows/ci.yml` — replaced in place.
* `scripts/install.sh` — new (POSIX installer for Linux/macOS,
  modeled on `mcp-server-magictools/scripts/install.sh`).
* `scripts/install.ps1` — new (Windows installer, modeled on
  `mcp-server-magictools/scripts/install.ps1`).
* `scripts/install_test.sh` — new (offline POSIX installer tests,
  modeled on `mcp-server-magictools/scripts/install_test.sh`).
* `Makefile` — no change required; existing `build-all`, `linux`,
  `darwin-arm64`, `windows-amd64` targets already match.
* `cmd/mcp-server-socratic-thinker/version.go` — no change required;
  `RawVersion`/`Version` already match.
* `.golangci.yml` — no change required.

### Items intentionally left out of scope

* Adopting `goreleaser`, Homebrew tap publication, or Docker image
  publication. These are larger fleet-level decisions and would
  deserve their own MADR if pursued.
* Branch protection changes (required checks). The workflow
  structure exposes a clean `release` job name to set as required,
  but the actual branch-protection configuration is a repository
  setting, not a code change.
* Adding a changelog generator or release-drafter. The magictools
  workflow relies on `gh release create --generate-notes`, which is
  sufficient for parity.
