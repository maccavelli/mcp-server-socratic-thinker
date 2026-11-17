---
status: accepted
date: 2026-11-17
decision-makers: Socratic Thinker maintainers
consulted: "govulncheck v1.9.0 (database updated 2026-11-11 02:51 UTC) under go1.27.4 and go1.26.11; golangci-lint v2.15.0 and v2.13.1 built into a scratch directory; actions/setup-go v7.0.0's README as read for the fleet's go-selfupdate-lib record 0018; the reusable publish workflow this repository calls, read at its pinned commit"
informed: Socratic Thinker contributors
---

# Build with Go 1.27.4 through the `toolchain` line, raise the floor to `go 1.26.11`, and run golangci-lint v2.15.0 in CI

<!-- markdownlint-disable MD013 MD024 -->

## Context and Problem Statement

The owner's instruction of 2026-11-17, verbatim, as it applies to this
repository (the list names thirteen repositories, this one last):

> "go-llmprovider-sdk, go-tui-lib, acp-go-sdk2, gobble-cli, magic-cli-remote, go-selfupdate-lib, mcp-server-duckduckgo, mcp-server-magictools, mcp-server-recall, mcp-server-sequential-thinking, mcplib, mcp-buntdb, and mcp-server-socratic-thinker all need to have their go floor pinned to 1.26.11, and go version/toolchain pinned to 1.27.4."

"Floor" is the `go` directive in `go.mod`; "version/toolchain" is the
`toolchain` line. A read-only survey found that this repository builds at
1.26.11. The owner then ruled, labels quoted exactly and only as labels:

* "One short record pair per repository, all approved now through CI
  (Recommended)": a pair is drafted in each ready repository, executed
  (`go.mod`, docs, gates) and pushed to `main` on that approval; tags and
  releases wait for the owner's word.
* "v2.15.0 everywhere, in each repository's Go-pins record (Recommended)":
  each record carries the CI lint pin, because golangci-lint v2.13.1
  cannot type-check code under Go 1.27.4 ("export data version 5 is
  greater than maximum supported version 4"); any new finding the newer
  linter reports goes to the owner with the code, and is not suppressed.

Why now: GO-2026-6859, in `net/http`, is reported under Go 1.27.3 and
1.26.10 and not under 1.27.4 and 1.26.11 (govulncheck v1.9.0, database of
2026-11-11, measured by the coordinator on 2026-11-17).

Everything in this record other than the quoted statement and labels is
evidence or the author's text.

**Evidence index** (HEAD `4466310`, clean tree, level with `origin/main`,
read 2026-11-17):

* **E1, the pins.** `go.mod:3` is `go 1.26.6`; there is no `toolchain`
  line. `Makefile:1` is `MOD_VERSION := 1.26.6`; no other line of the
  Makefile or the repository reads `MOD_VERSION` (searched with
  `grep -rn MOD_VERSION`), so the variable is dead and is moved to the
  floor so it stays true. `README.md` and the two earlier records state no
  Go version for the module (the earlier records mention Go only as a
  `go-version-file: go.mod` setting, and `docs/0002-MADR-*` cites the
  Go 1.24+ `os.Root` API, which is history).
* **E2, CI.** `ci.yml:54`, `:189` and `:266` use
  `go-version-file: go.mod` with `actions/setup-go` v7.0.0. Its README at
  that pin says: "If the `toolchain` directive is present, its version is
  used; otherwise, the action falls back to the `go` directive." (quoted
  from go-selfupdate-lib's 0018-MADR, which cites that README at commit
  `b7ad1da`). After the change CI therefore builds with Go 1.27.4. No job
  builds on the floor: there is no `go-version: '1.26.x'` job (see the gap
  below).
* **E3, the publish workflow.** `ci.yml:344` calls mcplib's
  `publish-selfupdate-release.yml` at commit `8d4d89f` (mcplib v1.4.1).
  Searching that file at that commit for `go-version`, `setup-go`,
  `go.mod`, `GOTOOLCHAIN` and `golang` finds nothing: it sets up no Go and
  reads no `go.mod` of this repository.
* **E4, the linter.** `ci.yml:89` installs
  `golangci-lint@v2.13.1`; the Makefile runs whatever `GOLANGCI_LINT`
  names (default `$(go env GOPATH)/bin/golangci-lint`) and its install hint
  says `@latest` (`Makefile:53`). Records 0001-MADR and 0001-PLAN name
  v2.13.1 as history.
* **E5, measurements** (this Mac; scratch copy of HEAD with the change):
  `go mod tidy -diff` exits 0 (nothing moves, `go.sum` unchanged);
  `govulncheck ./...` exits 0 under `GOTOOLCHAIN=go1.27.4` and under
  `go1.26.11` (`GOPROXY=off`), each printing "Your code is affected by 0
  vulnerabilities" and "1 vulnerability in packages you import and 18
  vulnerabilities in modules you require, but your code doesn't appear to
  call these". `-show verbose` lists them as `golang.org/x/text` and
  `golang.org/x/crypto` advisories, none called. golangci-lint v2.13.1
  (built with go1.27.4) on HEAD under `GOTOOLCHAIN=go1.26.6` reports 0
  issues; v2.15.0 under go1.27.4 and under go1.26.11 reports 0 issues, so
  the new linter adds no finding.
* **E6, requirements.** `github.com/maccavelli/mcplib` v1.4.1 (declares
  `go 1.26.6`) and `mcp-buntdb` v0.2.0 (declares `go 1.26.5`) are required
  by this module's graph at their current versions; neither is bumped, and
  both are at or below the new floor.

The question: should this module's `go.mod`, CI toolchain and CI linter
move as the owner instructed?

## Decision Drivers

* The owner's instruction and rulings above.
* Nothing ships on a standard library with a published advisory.
* The linter CI runs must be able to type-check the toolchain CI builds
  with.
* No gate is loosened and no finding is suppressed.
* No compatibility engineering: the owner is the only user of the fleet.

## Considered Options

* **A. `go 1.26.11`, `toolchain go1.27.4`, golangci-lint v2.15.0 (as ruled).**
* **B. A different floor** (keep `go 1.26.6`, add only the toolchain line).
* **C. Do nothing.**

## Decision Outcome

Chosen option: **"A"**, because it is the owner's ruling and the only
option under which CI builds with an advisory-free standard library and
its linter can still read the packages it checks.

1. `go.mod`: `go 1.26.6` becomes `go 1.26.11`; a `toolchain go1.27.4`
   line is added. No `require` changes.
2. `Makefile:1`: `MOD_VERSION := 1.26.11`.
3. `.github/workflows/ci.yml:89`: `golangci-lint@v2.15.0`. Nothing else in
   the workflow changes.
4. A release is not part of this decision (see More Information).

### Consequences

* Good, because CI's three `go.mod`-driven jobs build with Go 1.27.4 (E2)
  and the lint step can type-check them.
* Good, because the floor excludes 1.26.10 and earlier, which carry
  GO-2026-6859.
* Neutral, because no hosts change: each already runs Go 1.27.4 and the
  `toolchain` line only affects CI's `setup-go` steps.
* Neutral, because no module requires this one (a `main` package binary),
  so no consumer's minimum moves. Searched `go.mod` and `go.work` files
  under the fleet's Go directory for this module's path: only its own.
* Bad, because **CI has no job that builds and tests on the floor**: the
  floor declared in `go.mod` is checked only by the author's local run
  under `GOTOOLCHAIN=go1.26.11`. This is a named gap, not fixed here; the
  owner decides whether to add floor jobs fleet-wide.
* Bad, because `go.mod` now requires Go 1.26.11 of anyone building from
  source with `GOTOOLCHAIN=local`.

### Confirmation

* `go mod tidy -diff` exits 0.
* `govulncheck ./...` exits 0 under go1.27.4 and go1.26.11 (E5).
* The PLAN's version-agreement script passes on the tree and fails on
  three planted errors, including an old lint pin.
* The repository's gates pass, including `make lint` with a v2.15.0
  binary under both toolchains.
* CI on `main` shows `setup-go` installing 1.27.4 and a lint step that
  reports no type-check error (not yet observed).

## Pros and Cons of the Options

### A. Floor 1.26.11, toolchain 1.27.4, lint v2.15.0

* Good, because it matches the instruction and the measurements.
* Bad, because it raises the declared minimum by five patch releases.

### B. Keep the floor, add only the toolchain line

* Good, because the minimum does not move.
* Bad, because it is not what the owner instructed, and the floor then
  admits toolchains with GO-2026-6859.

### C. Do nothing

* Good, because nothing changes.
* Bad, because the lint step fails under Go 1.27.4 once any pin moves, and
  CI keeps building with no `toolchain` line (the `go` directive, 1.26.6).

## More Information

### Release

A tag and release need the owner's word; the PLAN stops before them. The
installed binary comes from a release asset (the `release` job builds on
a tag, `ci.yml:251`) or from a local `make install` (which uses the
local Go). The release asset is built with Go 1.27.4 only once a tag is
built from a commit carrying this change, so an installed binary benefits
only from a new release. Nothing here installs or restarts anything.

### Earlier records

0001-MADR-port-magictools-ci-cd-pipeline.md (status `superseded`, dated
2026-08-29) names `golangci-lint v2.13.1` and `go-version-file: go.mod`
as the pipeline's pins. That is history and stays untouched. This record
moves the lint pin by its own authority:

* **Amendment note (2026-11-17):** the CI lint pin that
  0001-MADR-port-magictools-ci-cd-pipeline.md names (`v2.13.1`) is
  `v2.15.0` from this record. 0002-MADR-remove-the-misleading-sanitizepath-helper.md
  states no Go version for the module and is unaffected.

### Not verified

* CI's own behaviour after the change: no CI run has been observed.
* That `setup-go` v7.0.0 installs 1.27.4 on the runner (the README is
  quoted second-hand from go-selfupdate-lib's record).
* v2.15.0's findings on other operating systems: lint ran on this Mac
  only (the repository's `.golangci.yml` sets no GOOS).
* What GO-2026-6859 is beyond its title.
* Whether the proxy was used for the one v2.15.0 install: the module
  cache already held it (`golangci-lint/v2/@v/v2.15.0.zip`).
