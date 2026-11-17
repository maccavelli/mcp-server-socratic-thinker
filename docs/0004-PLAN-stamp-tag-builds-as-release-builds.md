---
status: in-progress
date: 2026-11-17
associated-madr: "0004-MADR-stamp-tag-builds-as-release-builds.md"
decision-makers: Socratic Thinker maintainers
---

# Implement the release stamp in the tag build and the check that holds it

Associated MADR: [0004-MADR-stamp-tag-builds-as-release-builds.md](0004-MADR-stamp-tag-builds-as-release-builds.md)
(option A, as the owner ruled).

<!-- markdownlint-disable MD013 MD024 -->

## Goal

* The tag build stamps `RawBuildKind=release`.
* A check on every push fails if the workflow command or the Makefile
  default drifts.
* The README tells users of `v1.1.0` and `v1.1.1` what to do once.
* The change is ready to commit. The plan stops before any tag or release;
  both need the owner's word.

## Scope

| File | Change |
| :--- | :--- |
| `.github/workflows/ci.yml` | line 119 gains `BUILD_KIND=release`; a new always-on step "Release build flags" in the `go` job (after "Installer (POSIX)", before "Build release binaries") runs `shellcheck` and the script |
| `scripts/check_release_flags.sh` | new: the check |
| `README.md` | one paragraph under "Keeping it up to date" for local-stamped installs |
| `docs/0004-*` | this pair |

Out of scope: the Makefile (its default stays `local`, its comment's
"(MADR 0005)" refers to mcplib's record); any Go source; `--version`; the
other two servers; the tag and the release; any remote. No guide snippet
needed correcting (MADR E6).

## Implementation Steps

1. Write the script; run it against HEAD's unfixed workflow and see it fail.
2. Edit the workflow line, add the CI step, add the README paragraph, by a
   script that counts each old string before replacing it.
3. Run the script on the tree, under `sh` and `dash`, and on four scratch
   copies with planted breaks.
4. Gates: `actionlint`, `shellcheck`, `gofmt`, `go mod tidy -diff`,
   `make vet`, `make lint` with the scratch v2.15.0 under Go 1.27.4 and
   `GOTOOLCHAIN=go1.26.11`, `sh scripts/install_test.sh`, the tests through
   `run-heavy` under both toolchains, markdownlint on the changed docs.
5. Stage the two records only; the three change files stay unstaged.
6. **Release** (not in this plan's authority): after the coordinator
   commits and pushes and CI is green, and only on the owner's word, tag
   the next release; then read-only checks: the tag's CI green, the release
   published, checksums match, and the published binary, verified against
   `SHA256SUMS` and run as `update --check` in a scratch `HOME`, exits 0
   "up to date".

## Verification

* V1: the script passes on the tree and fails on HEAD and on the planted
  copies.
* V2: every gate passes.
* V3: after a tag, the runtime proof (step 6).

## Rollout and Rollback

Roll out by commit to `main` (the coordinator's), then CI. Before a tag,
revert the change commit; the records stay. After a tag a release is
immutable: a fault is fixed by the next patch tag. Users of `v1.1.0` and
`v1.1.1` change only when they run `update --force` or the installer.

## Execution record

All on 2026-11-17 at HEAD `bd479f7` plus this change, `GOPROXY=off`. Exit
statuses were taken before any filter.

* **Red first.** The script against a scratch copy of HEAD's workflow
  exited 1:
  `FAIL ci.yml's tag build does not pass BUILD_KIND=release: make
  build-all VERSION="$TAG"` and `FAIL workflow command expands to 3
  builds, 0 release, 3 local (want 3, 3, 0)`. (The runtime probe passed
  there: it builds with the flag given directly, so it does not test the
  workflow line.)
* **Probe established before relying on it** (scratch copy, scratch
  `HOME`, `env -i`, all `XDG_*` there, dead proxy so nothing left the
  machine): release-stamped `v0.0.0-test` binary, `update --check` exit 1,
  `"v0.0.0-test" is not a strict stable tag`; local-stamped, exit 1 with a
  `proxyconnect ... connection refused` network error (MADR E5).
* **The change:** `ci.yml:119` gains ` BUILD_KIND=release` (now `:128`
  after the inserted step); step "Release build flags" inserted at
  `ci.yml:106-114`; `scripts/check_release_flags.sh` new; `README.md`
  paragraph after the "mcplib MADR 0005" sentence.
* **On the tree:** four `ok` lines and "release build flags: pass", exit
  0, under `sh` and under `dash`.
* **Planted failures** (scratch copies of the changed tree), each exit 1:

  | Plant | Failing lines |
  | :--- | :--- |
  | flag removed from `ci.yml` | "does not pass BUILD_KIND=release"; "expands to 3 builds, 0 release, 3 local (want 3, 3, 0)" |
  | Makefile default `BUILD_KIND?=release` | "default build expands to 3 release, 0 local (want 0, 3)" |
  | windows target hard-coded to `local` | "expands to 3 builds, 2 release, 1 local (want 3, 3, 0)" |
  | darwin target hard-coded to `local` | the same expansion failure, and "built mcp-server-socratic-thinker-darwin-arm64 is not a release build: update --check did not fail version validation (exit 1)" |

* **Gates** (local times):

  | Gate | Start | End | Exit |
  | :--- | :--- | :--- | :--- |
  | `actionlint .github/workflows/ci.yml` | 15:39:14 | 15:39:14 | 0 |
  | `shellcheck -s sh scripts/check_release_flags.sh` | 15:39:14 | 15:39:14 | 0 |
  | script under `sh` | 15:39:14 | 15:39:16 | 0 |
  | script under `dash` | 15:39:16 | 15:39:17 | 0 |
  | `gofmt -l cmd internal` empty | 15:39:17 | 15:39:17 | 0 |
  | `go mod tidy -diff` | 15:39:17 | 15:39:17 | 0 |
  | `make vet` | 15:39:17 | 15:39:18 | 0 |
  | `make lint`, v2.15.0, go1.27.4 | 15:39:18 | 15:39:19 | 0 |
  | `make lint`, v2.15.0, go1.26.11 | 15:39:19 | 15:39:21 | 0 |
  | `sh scripts/install_test.sh` | 15:39:21 | 15:39:23 | 0 |
  | `go test -count=1 ./...`, go1.27.4, via `run-heavy` (includes queue) | 15:39:23 | 15:51:35 | 0 |
  | `go test -count=1 ./...`, go1.26.11, via `run-heavy` | 15:51:35 | 15:51:47 | 0 |

  Markdownlint on `README.md`: 0 issues. On copies of the two records,
  only MD004 (the `*` marker MADR requires; the repository's config
  excludes records).
* **Not done:** the tag and release; a run of the Linux probe (CI's
  host); a run of any released binary.
