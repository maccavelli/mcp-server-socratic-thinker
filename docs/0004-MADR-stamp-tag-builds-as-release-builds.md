---
status: accepted
date: 2026-11-17
decision-makers: Socratic Thinker maintainers
consulted: "mcp-server-recall's 0008-MADR and its change (the sibling that confirmed the runtime effect on a released binary); mcplib v1.4.1's selfupdate package, read; scratch builds of this repository run in a scratch HOME"
informed: Socratic Thinker contributors
---

# Stamp tag builds as release builds, and hold it with a check

<!-- markdownlint-disable MD013 MD024 -->

## Context and Problem Statement

At mcp-server-recall's release on 2026-11-17 its released binary was run in
a scratch home and found stamped as a local build, so its `update` refused
to self-update without `--force`. Three servers share the cause; this
repository is one. The owner ruled, as the coordinator relayed (label
quoted exactly and only as a label; the text beside it says what was
asked):

* **D1.** Chosen label: "Confirm with a scratch run, then fix all three
  and release recall v2.1.2 (Recommended)". Asked: three servers build
  release binaries with `make build-all VERSION="$TAG"` and no
  `BUILD_KIND=release`; confirm, then fix each. Asked separately whether
  recall's fixed release should go now, the owner chose "Wait until
  magictools and socratic-thinker have the same fix". Whether this
  repository is then released is the owner's separate word; the PLAN stops
  before any tag.

Everything in this record other than that quotation is evidence or the
author's text.

**Evidence index** (HEAD `bd479f7`, clean tree, 2026-11-17).

* **E1, the cause [read].** `.github/workflows/ci.yml:119` runs `make
  build-all VERSION="$TAG"` with no `BUILD_KIND`. `Makefile:10` defaults
  `BUILD_KIND?=local`; each build target stamps `-X
  main.RawBuildKind=$(BUILD_KIND)` (`Makefile:22`, `:28`, `:32`, `:36`).
  The Makefile's comment (`Makefile:6-9`) says the release workflow passes
  it explicitly; it does not.
* **E2, what the binary does with the stamp [read].**
  `cmd/mcp-server-socratic-thinker/version.go:10` defines
  `releaseBuildKind = "release"`, `:22` defaults `RawBuildKind = "local"`.
  `update.go:76-81` (`buildKind()`) returns `selfupdate.ReleaseBuild` only
  for the exact string "release", else `LocalBuild`, and `update.go:111`
  passes it as `CurrentBuild`. mcplib's selfupdate treats a local build as
  replaceable only with `--force`, and `--check` on a local build reports
  an actionable target at every version (the recall run measured exit 10
  "update available" at the latest version). Nothing else reads the kind.
  I did not download or run a released binary of this repository; the
  runtime effect was measured on recall and the code path is shared through
  mcplib.
* **E3, why CI did not catch it [read].** The tag build's smoke step
  asserts the version string only (`ci.yml:123-124`, repeated at `:245-246` and `:300-301`:
  `got="$("./$f" --version 2>&1 | awk '{print $NF}')"`, then `test "$got" =
  "$VER"`); the kind is not visible in `--version`.
* **E4, since which release [read].** The `update` command arrived in
  `067a438` ("feat(update): add the canonical self-update command"),
  contained in `v1.1.0` and `v1.1.1` and in no earlier tag (`v1.0.2` has no
  `update.go`). `git show v1.1.0:.github/workflows/ci.yml` and the same for
  `v1.1.1` both have `make build-all VERSION="$TAG"` at line 119. So
  `v1.1.0` and `v1.1.1` are affected, assuming their builds ran that line
  (not run here).
* **E5, scratch builds [measured].** In a scratch copy of HEAD and a scratch
  `HOME` (all four `XDG_*` variables pointed into it, `env -i`, the real
  home untouched), `make darwin-arm64 VERSION=v0.0.0-test` was built with
  `BUILD_KIND=release` and with `BUILD_KIND=local`, each copied under the
  scratch home, and run as `update --check` with a dead proxy
  (`HTTPS_PROXY=http://127.0.0.1:9`, so no request left the machine). The
  release-stamped binary exits 1 at once: `selfupdate: "v0.0.0-test" is
  not a strict stable tag` (version validation precedes any network call).
  The local-stamped one goes on and fails at the network: `Get
  "https://api.github.com/repos/.../releases/latest": proxyconnect tcp:
  dial tcp 127.0.0.1:9: connect: connection refused`. That difference is the
  runtime probe the check script uses. (A local-stamped binary outside the
  scratch home fails earlier with "is outside allowed roots", which the
  script also counts as "not a release build".)
* **E6, the checksum snippet [read, measured].** The defect recall's guide
  had (a snippet selecting a versioned file name while the release
  publishes plain names) does not exist here: there is no guide with such a
  snippet (`README.md` sends users to `scripts/install.sh`, and
  `docs/` holds only records). `v1.1.1`'s assets are
  `mcp-server-socratic-thinker-{darwin-arm64,linux-amd64,windows-amd64.exe}`
  and `SHA256SUMS` (`gh release view`, read-only); its `SHA256SUMS`,
  downloaded alone to scratch, lists those plain names, and
  `scripts/install.sh:88`'s canonical selection (`grep -E
  "  ${PRODUCT}-${OS}-${ARCH}$"`) finds exactly one line for linux-amd64.
  Nothing to correct.

The question: how should tag builds carry the release stamp, what keeps it
from regressing, and what do users of `v1.1.0` and `v1.1.1` do?

## Decision Drivers

* The stamp must be explicit and made by the workflow (mcplib's record on
  build identity: release workflows stamp both facts explicitly).
* A local build that merely resembles a tag must stay "local"
  (`Makefile:6-9`), so the Makefile must not infer the kind from `VERSION`.
* The guard must fail before a tag: a pushed tag makes an immutable
  release.
* No user-facing surface for the kind; `--version`'s last word is parsed by
  CI and the installers.
* No compatibility engineering: the owner is the only user of the fleet.

## Considered Options

* **A. The tag build passes `BUILD_KIND=release`** in `ci.yml`, plus a
  check on every push.
* **B. The Makefile infers "release" from a tag-shaped `VERSION`.**
* **C. Stamp in mcplib's reusable publish workflow.**
* **D. Change nothing; document `update --force`.**

## Decision Outcome

Chosen option: **"A"**, the owner's ruling D1.

1. **`ci.yml:119`** becomes `make build-all VERSION="$TAG" BUILD_KIND=release`.
2. **The check**, `scripts/check_release_flags.sh`, run on every push by a
   new step "Release build flags" in the `go` job (after the Installer
   step, which installs `shellcheck` if the runner lacks it; the new step
   runs `shellcheck -s sh` on the script first). It extracts the workflow's
   `make build-all` line, requires `BUILD_KIND=release` on it, requires
   that line's `make -n` expansion to carry `main.RawBuildKind=release` on
   all three targets (`linux`, `darwin-arm64`, `windows-amd64`) and no
   `local`, requires `make -n build-all VERSION=v1.2.3` without the flag to
   carry `local` on all three, and, on Linux amd64 or macOS arm64, builds
   one target with `BUILD_KIND=release VERSION=v0.0.0-test` and requires its
   `update --check` to fail with "is not a strict stable tag" (E5).
3. **No user-facing surface is added.**
4. **Users of `v1.1.0` and `v1.1.1`:** their binaries are local-stamped and
   stay so; they run `update --force` once, or the installer, after which
   the installed binary is a release build and updates normally. The README
   says so ("Keeping it up to date").
5. **A release follows only on the owner's word.** The PLAN stops before the
   tag.

### Consequences

* Good, because every tag build from the next release is a release build
  and `update` works as designed.
* Good, because the check fails before a tag if the flag or the Makefile's
  default drifts.
* Bad, because the probe builds one binary per push on Linux amd64 (one
  extra build).
* Bad, because the workflow's own tag build is proved only by the command's
  expansion and, after a tag, by a run of the published binary: the kind is
  not visible in a binary, and the probe builds with the flag given
  directly.
* Bad, because existing `v1.1.0` and `v1.1.1` installs need one `--force`
  or an installer run; no release can change them.
* Neutral, because the next release changes no Go source.
* Neutral, because the Makefile comment's "(MADR 0005)" refers to mcplib's
  record and is not edited here.

### Confirmation

* `sh scripts/check_release_flags.sh` passes on the tree and under `dash`;
  against HEAD's unfixed workflow it exits 1; and it exits 1 on scratch
  copies with the flag removed from `ci.yml`, with the Makefile default set
  to `release`, with the windows target hard-coded to `local`, and with the
  darwin target hard-coded to `local` (the last also fails the probe). The
  PLAN's execution record quotes each.
* `actionlint`, `shellcheck`, `make vet`, `make lint` with v2.15.0 under
  Go 1.27.4 and `go1.26.11`, and the tests pass.
* After a tag, on the owner's word: the tag's CI passes and the published
  binary, run in a scratch home, reports "up to date" with exit 0 on
  `update --check`.

## Pros and Cons of the Options

### A. Pass the flag in the workflow (chosen)

* Good, because it is one explicit line, as mcplib asks, and matches the
  sibling servers' fix.
* Bad, because a person can drop it again; hence the check.

### B. Infer the kind from a tag-shaped `VERSION`

* Good, because there is nothing for a workflow to forget.
* Bad, because `Makefile:6-9` argues against it: a local build whose
  `git describe` resembles a tag would claim to be a release and `update`
  would trust it; mcplib keeps the kind explicit.

### C. Stamp in the reusable workflow

* Good, because it would fix every caller at once.
* Bad, because the reusable workflow publishes artifacts this job already
  built; the stamp is in the binary before it gets there.

### D. Change nothing, document `--force`

* Good, because nothing changes.
* Bad, because every release binary stays a local build and `update
  --check` reports "update available" forever.

## More Information

Relates to 0003-MADR-move-toolchain-to-go-1-27-4-and-floor-to-1-26-11.md,
which moved the pins; this record changes none of them. Its plan is
0004-PLAN-stamp-tag-builds-as-release-builds.md.

### Not verified

* That `v1.1.0` and `v1.1.1`'s release binaries are local-stamped: derived
  from their workflow line and the shared code path; no released binary of
  this repository was downloaded or run.
* That a tag build with the flag reports a release build at run time: shown
  for the command, its expansion and a scratch build, not for a tag.
* That `update --force` from a local build succeeds end to end: read from
  mcplib's source as the recall record did, not run here.
* `scripts/install.ps1`'s checksum selection was not read for this record.
* The probe on Linux amd64 (CI): run on macOS arm64 only.
