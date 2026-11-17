---
status: in-progress
date: 2026-11-17
associated-madr: "0003-MADR-move-toolchain-to-go-1-27-4-and-floor-to-1-26-11.md"
decision-makers: Socratic Thinker maintainers
---

# Implement the Go 1.27.4 toolchain, the `go 1.26.11` floor and the golangci-lint v2.15.0 CI pin

Associated MADR: [0003-MADR-move-toolchain-to-go-1-27-4-and-floor-to-1-26-11.md](0003-MADR-move-toolchain-to-go-1-27-4-and-floor-to-1-26-11.md)

<!-- markdownlint-disable MD013 MD024 -->

## Goal

`go.mod` says `go 1.26.11` and `toolchain go1.27.4`; every live pin and
current statement agrees; CI installs golangci-lint v2.15.0; every gate
passes, with v2.15.0 reporting no finding.

## Scope

| File | Line | Old | New |
| :--- | :--- | :--- | :--- |
| `go.mod` | 3 | `go 1.26.6` | `go 1.26.11` |
| `go.mod` | 5 (new) | none | `toolchain go1.27.4` |
| `Makefile` | 1 | `MOD_VERSION := 1.26.6` | `MOD_VERSION := 1.26.11` |
| `.github/workflows/ci.yml` | 89 | `golangci-lint@v2.13.1` | `golangci-lint@v2.15.0` |

Out of scope: `mcplib` and `mcp-buntdb` versions; any other workflow
line; history (records 0001 and 0002, which name `v2.13.1` and other
versions as history); a floor CI job (named gap in the MADR); tags and
releases; any remote.

No README, guide or architecture page states a Go version for the module
(searched `README.md`, `docs/`, `scripts/`). There is no changelog.

## Implementation Steps

1. Edit the four lines by a Python script that asserts each old string's
   count before replacing; read the result back.
2. `go mod tidy -diff` exits 0 (go.sum unchanged).
3. Version-agreement check (scratch script, not added to the tree): reads
   `go` and `toolchain` from `go.mod`, asserts `Makefile` `MOD_VERSION`
   equals the floor, the CI lint pin is `v2.15.0`, `ci.yml` keeps
   `go-version-file: go.mod`, and no live file outside `docs/` and
   `go.sum` names another Go version or lint version. It must fail on
   scratch copies with a wrong toolchain, a wrong floor and an old lint
   pin.
4. Gates: `make fmt` (no diff), `make vet`, `make lint` with a v2.15.0
   binary under go1.27.4 and go1.26.11, `make test` through `run-heavy`,
   the floor build, vet and tests under `GOTOOLCHAIN=go1.26.11
   GOPROXY=off`, `govulncheck ./...` under both toolchains, cross-builds
   for linux and windows with `CGO_ENABLED=0`.
5. Stage the two records only. Leave the three change files unstaged. No
   commit.

## Verification

The gates above; the agreement script; the MADR's Confirmation list. CI
after push (not part of this plan's author run).

## Rollout and Rollback

Roll out by commit to `main` by the coordinator, then CI. Stop before any
tag. Roll back with `git revert` of the change commit; nothing outside the
tree changes.

## Execution record

Filled in 2026-11-17.

* **Baseline.** `make lint` with golangci-lint v2.13.1 on a scratch copy
  of HEAD under `GOTOOLCHAIN=go1.26.6`: exit 0, 0 issues.
* **Measurements on the changed scratch copy.** `go mod tidy -diff` exit 0;
  `govulncheck` exit 0 under go1.27.4 and go1.26.11, 0 affected, 1
  imported-package and 18 required-module advisories not called; `make
  lint` with v2.15.0 exit 0, 0 issues under both toolchains.
* **Version-agreement check:** passes on the changed tree ("agree"); on
  scratch copies it fails on `go.mod: toolchain 1.27.3 != 1.27.4`, on
  `go.mod: floor 1.26.10 != 1.26.11` (and `Makefile:1: MOD_VERSION
  1.26.11 != floor 1.26.10`), and on `.github/workflows/ci.yml:89: lint
  pin v2.13.1 != v2.15.0`; it also fails on unchanged HEAD.
* **Gates** (2026-11-17, local times, after the last non-record edit;
  exit status taken before any filter). The test runs went through
  `run-heavy` and include queue time.

  | Gate | Start | End | Exit |
  | :--- | :--- | :--- | :--- |
  | `go mod tidy -diff` | 14:32:27 | 14:32:27 | 0 |
  | `gofmt -l cmd internal` empty | 14:32:27 | 14:32:27 | 0 |
  | `make vet` | 14:32:27 | 14:32:28 | 0 |
  | `make lint`, v2.15.0, go1.27.4 | 14:32:28 | 14:32:30 | 0 (0 issues) |
  | `make lint`, v2.15.0, go1.26.11 | 14:32:30 | 14:32:33 | 0 (0 issues) |
  | `CGO_ENABLED=0 go test -count=1 -timeout 20m ./...` | 14:32:33 | 15:07:53 | 0 |
  | floor build and vet, go1.26.11, `GOPROXY=off` | 15:07:53 | 15:07:56 | 0 |
  | floor `go test -count=1 -timeout 20m ./...`, go1.26.11 | 15:07:56 | 15:09:58 | 0 |
  | `govulncheck ./...`, go1.27.4 | 15:09:58 | 15:10:01 | 0 |
  | `govulncheck ./...`, go1.26.11 | 15:10:01 | 15:10:04 | 0 |
  | cross-build linux/amd64, `CGO_ENABLED=0` | 15:10:04 | 15:10:06 | 0 |
  | cross-build windows/amd64, `CGO_ENABLED=0` | 15:10:06 | 15:10:08 | 0 |

* **Not run:** `make test` (it is `go test -v ./...`, covered by the
  `go test` rows), `-race`, and the darwin cross-build (this Mac builds
  it natively). No record-tooling or disclosure check exists here; the
  repository's markdownlint config excludes MADR and PLAN files, and a
  run of it on copies of these two files reports only MD004 (the `*` list
  marker that MADR requires).
* **Status:** the plan stops here. Staging and commit are the
  coordinator's; no tag or release.
