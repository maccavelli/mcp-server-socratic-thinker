---
status: proposed
date: 2026-08-29
---

# Implement the hardened magictools-style CI/CD pipeline in mcp-server-socratic-thinker

Associated MADR: [0001-MADR-port-magictools-ci-cd-pipeline.md](0001-MADR-port-magictools-ci-cd-pipeline.md)

## Goal

Replace the minimal three-job Linux-only release workflow at
`.github/workflows/ci.yml` with the four-job, three-OS hardened pipeline
used by `mcp-server-magictools`, and add the three matching installer
scripts so that the socratic-thinker release story is identical to the
rest of the fleet. After this plan is executed, a tag of the form
`vX.Y.Z` produces a GitHub Release whose every asset is built, tested,
and smoke-executed on its native OS in CI before upload.

## Scope

### Files created

| Path | Purpose |
|---|---|
| `.github/workflows/ci.yml` | Replacement workflow (4 jobs: `go`, `go-native`, `smoke-native`, `release`) |
| `scripts/install.sh` | POSIX installer for Linux + macOS |
| `scripts/install.ps1` | PowerShell installer for Windows |
| `scripts/install_test.sh` | Offline POSIX installer tests run inside CI |

### Files modified

None. The existing `Makefile`, `cmd/mcp-server-socratic-thinker/version.go`,
and `.golangci.yml` already match the magictools shape per the MADR's
"anchors that already match" section, and require no changes.

### Files deleted

The current `.github/workflows/ci.yml` (72 lines) is overwritten in
place by the new content; no backup copy is kept in the tree (git
history preserves it).

### Explicitly out of scope

* `goreleaser`, Homebrew tap, Docker images, archives. Reserved for a
  later MADR.
* Branch-protection rule changes. The release-job name `release` is
  suitable for setting as a required check; that is a repo setting, not
  a code change.
* Any Go source change. The plan touches YAML and shell/PowerShell
  only.

## Implementation Steps

The plan is organized in five phases. Each phase produces files that
can be committed independently. Per `~/.config/opencode/AGENTS.md`,
commit at the end of every phase after the pre-commit checks pass.

### Phase 1 — Workflow file (`.github/workflows/ci.yml`)

**Action**: Overwrite `.github/workflows/ci.yml` with the content below
(342 lines, adapted from
`mcp-server-magictools/.github/workflows/ci.yml`).

**Substitutions applied** (mechanical rename, no logic change):

| Magictools value | Socratic-thinker value |
|---|---|
| `mcp-server-magictools` (binary / artifact name) | `mcp-server-socratic-thinker` |
| `~/.local/bin` (default POSIX install dir) | unchanged |
| `Programs\magictools` (Windows default install dir, `install.ps1` line 111 + `go-native` dry-run assertion line 210) | `Programs\socratic-thinker` |
| `MCP_MAGICTOOLS_VERSION` (env var) | `MCP_SOCRATIC_THINKER_VERSION` |
| `MCP_MAGICTOOLS_INSTALL_DIR` (env var) | `MCP_SOCRATIC_THINKER_INSTALL_DIR` |
| `MC_TEST_BASE_URL` (test-only env var, in `install.sh` only) | `ST_TEST_BASE_URL` |
| `.magictools-install.XXXXXX` (mktemp pattern in `install.sh`) | `.socratic-thinker-install.XXXXXX` |
| `magictools-install-` (mktemp pattern in `install.ps1`) | `socratic-thinker-install-` |

**New file content** (replace `~/.github/workflows/ci.yml`):

```yaml
name: CI

on:
  push:
    branches: ['**']
    tags:
      - "v*"
  pull_request:
  workflow_dispatch:

# One run per ref; newer branch/PR pushes supersede older runs. Tag runs are
# never cancelled because cancellation during upload can leave partial releases.
concurrency:
  group: ci-${{ github.workflow }}-${{ github.ref }}
  cancel-in-progress: ${{ github.ref_type != 'tag' }}

# Prefer Node.js 24 LTS for JavaScript-based Actions. Do not use Node 20.
env:
  FORCE_JAVASCRIPT_ACTIONS_TO_NODE24: true
  NODE_VERSION: "24"

permissions:
  contents: read

jobs:
  # Linux owns repository-wide quality checks and produces release artifacts.
  go:
    name: Go (linux/amd64; build on tag)
    runs-on: ubuntu-24.04
    timeout-minutes: 25
    outputs:
      version: ${{ steps.build.outputs.version }}
    steps:
      - name: Checkout
        uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          fetch-depth: 0

      - name: Set up Node.js (LTS 24)
        uses: actions/setup-node@820762786026740c76f36085b0efc47a31fe5020 # v7.0.0
        with:
          node-version: ${{ env.NODE_VERSION }}
          check-latest: true

      - name: Assert Node.js major version
        run: |
          node -v
          major="$(node -p "process.versions.node.split('.')[0]")"
          test "$major" = "24" || { echo "expected Node 24, got $(node -v)" >&2; exit 1; }

      - name: Set up Go
        uses: actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0
        with:
          go-version-file: go.mod
          cache: true

      - name: Gofmt
        run: |
          set -euo pipefail
          unformatted="$(gofmt -l cmd internal)"
          if [ -n "$unformatted" ]; then
            echo "error: gofmt drift — run 'make fmt':" >&2
            echo "$unformatted" >&2
            exit 1
          fi

      - name: Go mod tidy is clean
        run: |
          set -euo pipefail
          cp go.mod /tmp/go.mod.bak
          cp go.sum /tmp/go.sum.bak
          go mod tidy
          if ! diff -q go.mod /tmp/go.mod.bak >/dev/null || ! diff -q go.sum /tmp/go.sum.bak >/dev/null; then
            echo "error: go.mod/go.sum are not tidy — run 'go mod tidy' and commit" >&2
            diff -u /tmp/go.mod.bak go.mod || true
            diff -u /tmp/go.sum.bak go.sum || true
            exit 1
          fi

      - name: Vet
        run: go vet ./...

      - name: Test (cgo-free, all packages)
        run: CGO_ENABLED=0 go test ./...

      - name: Lint
        run: |
          set -euo pipefail
          go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.1
          GOLANGCI_LINT="$(go env GOPATH)/bin/golangci-lint" make lint

      - name: Installer (POSIX)
        run: |
          set -euo pipefail
          sh -n scripts/install.sh
          tail -n 1 scripts/install.sh | grep -qx 'main "$@"'
          if command -v shellcheck >/dev/null 2>&1; then
            shellcheck -s sh scripts/install.sh
          else
            sudo apt-get update -qq
            sudo apt-get install -y -qq shellcheck
            shellcheck -s sh scripts/install.sh
          fi
          sh scripts/install_test.sh

      - name: Build release binaries
        id: build
        if: github.ref_type == 'tag'
        run: |
          set -euo pipefail
          TAG="${GITHUB_REF_NAME}"
          if [[ ! "$TAG" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
            echo "error: tag '$TAG' is not vX.Y.Z" >&2
            exit 1
          fi
          VER="${TAG#v}"
          echo "version=$VER" >> "$GITHUB_OUTPUT"

          make build-all VERSION="$TAG"

          f="dist/mcp-server-socratic-thinker-linux-amd64"
          test -f "$f" || { echo "error: missing $f" >&2; exit 1; }
          got="$("./$f" --version 2>&1 | awk '{print $NF}')"
          test "$got" = "$VER" || {
            echo "error: linux-amd64 reports '$got', expected '$VER'" >&2
            exit 1
          }

          for bin in dist/mcp-server-socratic-thinker-*; do
            if ! go version -m "$bin" | grep -q '^	build	CGO_ENABLED=0$'; then
              echo "error: $bin is not CGO_ENABLED=0 — refusing to publish" >&2
              go version -m "$bin" | grep -i cgo >&2 || true
              exit 1
            fi
            echo "ok: $bin is cgo-free"
          done

          (
            cd dist
            rm -f SHA256SUMS SHA256SUMS.tmp
            set --
            for f in *; do
              [ -f "$f" ] || continue
              set -- "$@" "$f"
            done
            [ "$#" -gt 0 ] || { echo "no build artifacts to checksum" >&2; exit 1; }
            sha256sum "$@" > SHA256SUMS.tmp
            mv SHA256SUMS.tmp SHA256SUMS
            cat SHA256SUMS
          )
          ls -lh dist/
          {
            echo "### Go binaries"
            echo "Stamped: \`$VER\` from tag \`$TAG\`"
            echo "Platforms: linux/amd64, darwin/arm64, windows/amd64"
          } >> "$GITHUB_STEP_SUMMARY"

      - name: Upload Go binaries
        if: github.ref_type == 'tag'
        uses: actions/upload-artifact@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a # v7.0.1
        with:
          name: mcp-server-socratic-thinker-${{ steps.build.outputs.version || github.sha }}
          path: dist/
          if-no-files-found: error

  # Cross-compiling proves a target links; native execution proves it works.
  go-native:
    name: Go (${{ matrix.label }})
    strategy:
      fail-fast: false
      matrix:
        include:
          - { runner: macos-15, label: darwin/arm64, shell: bash }
          - { runner: windows-2025, label: windows/amd64, shell: bash }
    runs-on: ${{ matrix.runner }}
    timeout-minutes: 30
    env:
      CGO_ENABLED: "0"
    defaults:
      run:
        shell: ${{ matrix.shell }}
    steps:
      - name: Checkout
        uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1

      - name: Set up Go
        uses: actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0
        with:
          go-version-file: go.mod
          cache: true

      - name: Build
        run: go build ./...

      - name: Vet
        run: go vet ./...

      - name: Test
        run: go test ./...

      - name: Installer (PowerShell dry run)
        if: runner.os == 'Windows'
        shell: pwsh
        run: |
          $output = & ./scripts/install.ps1 -WhatIf 6>&1 | Out-String
          Write-Host $output
          if ($output -match '(?i)configure|service install') {
            throw 'installer must only place the binary'
          }
          if ($output -notmatch [regex]::Escape('Programs\socratic-thinker')) {
            throw 'installer did not report the expected installation directory'
          }

  # Execute tag-built non-Linux binaries on their target operating systems.
  smoke-native:
    name: Smoke (${{ matrix.label }})
    if: github.ref_type == 'tag'
    needs: [go]
    strategy:
      fail-fast: false
      matrix:
        include:
          - { runner: macos-15, label: darwin/arm64, suffix: darwin-arm64, ext: "" }
          - { runner: windows-2025, label: windows/amd64, suffix: windows-amd64, ext: ".exe" }
    runs-on: ${{ matrix.runner }}
    timeout-minutes: 15
    steps:
      - name: Download Go binaries
        uses: actions/download-artifact@3e5f45b2cfb9172054b4087a40e8e0b5a5461e7c # v8
        with:
          name: mcp-server-socratic-thinker-${{ needs.go.outputs.version || github.sha }}
          path: dist

      - name: Smoke binaries
        shell: bash
        env:
          VER: ${{ needs.go.outputs.version }}
          SUFFIX: ${{ matrix.suffix }}
          EXT: ${{ matrix.ext }}
        run: |
          set -euo pipefail
          f="dist/mcp-server-socratic-thinker-${SUFFIX}${EXT}"
          test -f "$f" || { echo "error: $f missing from the artifact" >&2; exit 1; }
          chmod +x "$f" || true
          got="$("./$f" --version 2>&1 | awk '{print $NF}')"
          test "$got" = "$VER" || {
            echo "error: $f reports '$got', expected '$VER'" >&2; exit 1; }
          echo "ok: $f $got"

  # Publish tag assets only after quality, native tests, and native smoke pass.
  release:
    name: Publish Release assets
    if: github.ref_type == 'tag'
    needs: [go, go-native, smoke-native]
    runs-on: ubuntu-24.04
    timeout-minutes: 15
    permissions:
      contents: write
    steps:
      - name: Checkout
        uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1

      - name: Set up Go
        uses: actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0
        with:
          go-version-file: go.mod
          cache: false

      - name: Download Go binaries
        uses: actions/download-artifact@3e5f45b2cfb9172054b4087a40e8e0b5a5461e7c # v8
        with:
          name: mcp-server-socratic-thinker-${{ needs.go.outputs.version || github.sha }}
          path: go-bin

      - name: Attach to GitHub Release
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          GH_REPO: ${{ github.repository }}
        run: |
          set -euo pipefail
          TAG="${{ github.ref_name }}"
          VER="${{ needs.go.outputs.version || github.sha }}"
          NAME="mcp-server-socratic-thinker"

          chmod +x go-bin/"${NAME}"-* || true

          (cd go-bin && sha256sum -c SHA256SUMS)

          for f in go-bin/"${NAME}"-*; do
            if ! go version -m "$f" | grep -q '^	build	CGO_ENABLED=0$'; then
              echo "error: $f is not CGO_ENABLED=0 — refusing to publish" >&2
              go version -m "$f" | grep -i cgo >&2 || true
              exit 1
            fi
            echo "ok: $f is cgo-free"
          done

          f="go-bin/${NAME}-linux-amd64"
          got="$("./$f" --version 2>&1 | awk '{print $NF}')"
          test "$got" = "$VER" || {
            echo "error: linux-amd64 reports version '$got', expected '$VER'" >&2
            exit 1
          }

          ASSETS=()
          for f in go-bin/"${NAME}"-*; do
            b="$(basename "$f")"
            case "$b" in
              *.exe) out="${b%.exe}-${VER}.exe" ;;
              *)     out="${b}-${VER}" ;;
            esac
            cp -f "$f" "$out"
            ASSETS+=("$out")
          done

          SUMS="SHA256SUMS-${VER}"
          sha256sum "${ASSETS[@]}" > "$SUMS"
          ASSETS+=("$SUMS")

          for f in "${NAME}"-*-"${VER}"*; do
            [ -f "$f" ] || continue
            base="${f%%-"${VER}"*}"
            ext="${f#*-"${VER}"}"
            alias_name="${base}${ext}"
            cp -f "$f" "$alias_name"
            ASSETS+=("$alias_name")
          done
          cp -f "$SUMS" SHA256SUMS
          ASSETS+=("SHA256SUMS")

          cp -f scripts/install.sh install.sh
          ASSETS+=("install.sh")
          cp -f scripts/install.ps1 install.ps1
          ASSETS+=("install.ps1")

          ls -lh "${ASSETS[@]}"
          cat "$SUMS"

          if ! gh release view "$TAG" >/dev/null 2>&1; then
            gh release create "$TAG" --generate-notes --title "$TAG"
          fi
          gh release upload "$TAG" "${ASSETS[@]}" --clobber
```

**Local validation for Phase 1**:

```sh
# YAML parses
python3 -c "import yaml,sys; yaml.safe_load(open('.github/workflows/ci.yml'))"
# (or) actionlint .github/workflows/ci.yml  # if installed
```

### Phase 2 — POSIX installer (`scripts/install.sh`)

**Action**: Create `scripts/install.sh` (chmod 0755) by copying
`mcp-server-magictools/scripts/install.sh` and applying the
substitutions below. Each substitution is a `sed`/`Edit` operation;
do not alter logic.

**Substitutions** (one Edit per row, in order):

1. Header comment `mcp-server-magictools` → `mcp-server-socratic-thinker`
   (top-of-file docstring).
2. Header comment `mcp-server-magictools/releases/latest/download/install.sh | sh`
   → `mcp-server-socratic-thinker/releases/latest/download/install.sh | sh`.
3. `REPO_URL="https://github.com/maccavelli/mcp-server-magictools/releases"`
   → `REPO_URL="https://github.com/maccavelli/mcp-server-socratic-thinker/releases"`.
4. `PRODUCT="mcp-server-magictools"` → `PRODUCT="mcp-server-socratic-thinker"`.
5. `.magictools-install.XXXXXX` → `.socratic-thinker-install.XXXXXX`.
6. `MCP_MAGICTOOLS_VERSION` → `MCP_SOCRATIC_THINKER_VERSION` (twice: once
   in `--version` arg-parser, once in `usage` doc).
7. `MCP_MAGICTOOLS_INSTALL_DIR` → `MCP_SOCRATIC_THINKER_INSTALL_DIR` (twice:
   once in `--dir` arg-parser, once in `usage` doc).
8. `MC_TEST_BASE_URL` → `ST_TEST_BASE_URL` (only inside `download_all`
   and `main`; the test-only env var name change avoids fleet
   contamination).
9. Windows pointer comment
   `Windows: irm https://github.com/maccavelli/mcp-server-magictools/releases/latest/download/install.ps1 | iex`
   → replace `mcp-server-magictools` with `mcp-server-socratic-thinker`.
10. `usage` doc header `mcp-server-magictools Linux / macOS installer`
    → `mcp-server-socratic-thinker Linux / macOS installer`.
11. Final `log` line referencing `$PRODUCT` stays unchanged because
    `$PRODUCT` itself was substituted in step 4.

**Local validation for Phase 2**:

```sh
chmod +x scripts/install.sh
sh -n scripts/install.sh
tail -n 1 scripts/install.sh | grep -qx 'main "$@"'
command -v shellcheck >/dev/null && shellcheck -s sh scripts/install.sh
```

### Phase 3 — PowerShell installer (`scripts/install.ps1`)

**Action**: Create `scripts/install.ps1` by copying
`mcp-server-magictools/scripts/install.ps1` and applying:

1. `.SYNOPSIS` / `.DESCRIPTION`: `mcp-server-magictools` →
   `mcp-server-socratic-thinker`; `%LOCALAPPDATA%\Programs\magictools`
   → `%LOCALAPPDATA%\Programs\socratic-thinker`.
2. `[string]$BaseUrl = 'https://github.com/maccavelli/mcp-server-magictools/releases'`
   → `[string]$BaseUrl = 'https://github.com/maccavelli/mcp-server-socratic-thinker/releases'`.
3. `$Product = 'mcp-server-magictools'` → `$Product = 'mcp-server-socratic-thinker'`.
4. Default `InstallDir` computation
   `Join-Path $env:LOCALAPPDATA 'Programs\magictools'`
   → `Join-Path $env:LOCALAPPDATA 'Programs\socratic-thinker'`.
5. Mktemp prefix `'magictools-install-'` → `'socratic-thinker-install-'`.

**Local validation for Phase 3**: PowerShell is not required on Linux;
skip locally. CI exercises the script on `windows-2025` via
`& ./scripts/install.ps1 -WhatIf` (assertion baked into
`go-native.Installer (PowerShell dry run)` step).

### Phase 4 — POSIX installer tests (`scripts/install_test.sh`)

**Action**: Create `scripts/install_test.sh` (chmod 0755) by copying
`mcp-server-magictools/scripts/install_test.sh` and applying:

1. `PRODUCT=mcp-server-magictools` → `PRODUCT=mcp-server-socratic-thinker`.
2. In `mk_release`, the per-OS binary filename pattern
   `"$PRODUCT-$os-$arch"` stays the same (already parameterized).
3. `MC_TEST_BASE_URL` → `ST_TEST_BASE_URL` (in the `run_installer`
   helper, line ~75).
4. `.magictools-install.*` → `.socratic-thinker-install.*` in any glob
   matchers.
5. `MCP_MAGICTOOLS_INSTALL_DIR` → `MCP_SOCRATIC_THINKER_INSTALL_DIR`
   in the `run_installer` helper.
6. In the test-fixture binary, change the echoed string
   `"$PRODUCT version $VER"` stays unchanged because `$PRODUCT` was
   substituted. The fixture prints `mcp-server-socratic-thinker
   version 9.9.9` to stdout — that is what `printVersion()` would
   emit (after `awk '{print $NF}'`), so the assertion logic is
   unaffected.

**Required test cases** (must all pass):

| # | Case | Expected exit | Assertion |
|---|---|---|---|
| 1 | linux/amd64 dry run | 0 | output contains `os:          linux`, `arch:        amd64`, `action:      place binary only` |
| 2 | linux/arm64 dry run | 1 | output contains `linux/arm64 is not a published target` |
| 3 | darwin/arm64 dry run | 0 | output contains `os:          darwin`, `arch:        arm64` |
| 4 | darwin/amd64 dry run | 1 | output contains `darwin/amd64 is not a published target` |
| 5 | FreeBSD dry run | 1 | output contains `install.ps1 | iex` (points Windows users to PowerShell) |
| 6 | missing checksum entry | 2 | nothing installed |
| 7 | corrupt binary | 2 | existing binary untouched, temp dir removed |
| 8 | valid release install | 0 | binary is executable, no `.executed` side-effect file, no `*configure*` / `*service*` files, `$VER` reported |
| 9 | reinstall | 0 | previous binary retained as `.prev`, no execution side-effect, temp dir removed |
| 10 | pinned version install | 0 | `--version v$VER` succeeds |
| 11 | invalid version | 1 | `--version nope` rejected |
| 12 | script ends with `main "$@"` | n/a | `tail -n 1 scripts/install.sh` equals `main "$@"` |

**Local validation for Phase 4**:

```sh
chmod +x scripts/install_test.sh
sh -n scripts/install_test.sh
sh scripts/install_test.sh
```

Expect output like `12 passed, 0 failed`.

### Phase 5 — Repository state and final commit

**Action**: Confirm pre-commit hygiene. Per `~/.config/opencode/AGENTS.md`,
the `precommit-gate.ts` hook enforces `gofmt`/`golint` only on Go and
Dart files. This plan touches YAML and shell/PowerShell only, so the
hook will not block. Run the equivalent checks locally anyway:

```sh
gofmt -l cmd internal                       # expect empty
go vet ./...                                # expect exit 0
CGO_ENABLED=0 go test ./...                 # expect exit 0
golangci-lint run -c .golangci.yml ./...    # expect exit 0 (requires v2.13.1)
go mod tidy && git diff --exit-code go.mod go.sum   # expect no diff
shellcheck -s sh scripts/install.sh         # expect exit 0
sh scripts/install_test.sh                  # expect "0 failed"
```

If any check fails, fix the corresponding file and re-run. Do not
amend or skip commits.

## Verification

### Pre-merge (feature branch)

After Phase 5 lands on a feature branch, push the branch and observe
the GitHub Actions run:

* `go` job passes (Linux quality + installer dry-run + install_test.sh).
* `go-native` matrix passes on macos-15 and windows-2025. The Windows
  runner additionally runs `install.ps1 -WhatIf` and asserts the
  output reports `Programs\socratic-thinker` and contains no
  `configure`/`service install` text.
* `release` and `smoke-native` jobs are correctly skipped on a
  non-tag ref.

### Tag validation negative test

Without merging to `main`, push a malformed tag to verify the regex
guard:

```sh
git tag v1.2.3-rc1
git push origin v1.2.3-rc1
```

Expected: `go.Build release binaries` step fails with
`error: tag 'v1.2.3-rc1' is not vX.Y.Z`. Delete the tag after the test:

```sh
git push origin :refs/tags/v1.2.3-rc1
git tag -d v1.2.3-rc1
```

### Tag validation positive test (real release)

Merge to `main`, then push the real release tag:

```sh
git checkout main
git merge --no-ff <feature-branch>
git tag vX.Y.Z
git push origin vX.Y.Z
```

Expected GitHub Actions run:

* `go` builds linux/amd64, darwin/arm64, windows/amd64 binaries;
  linux/amd64 reports `--version X.Y.Z`; every binary is cgo-free;
  SHA256SUMS written; artifact uploaded.
* `go-native` matrix passes on macos-15 and windows-2025.
* `smoke-native` matrix downloads the artifact on macos-15 and
  windows-2025 and asserts `--version` equality.
* `release` re-checksums against SHA256SUMS, re-asserts cgo-free,
  re-asserts linux/amd64 `--version`, stages versioned + alias
  copies, writes `SHA256SUMS-$VER` and `SHA256SUMS`, attaches
  `install.sh` and `install.ps1`, creates the release, uploads
  with `--clobber`.

Expected GitHub Release asset list:

```
mcp-server-socratic-thinker-linux-amd64-<X.Y.Z>
mcp-server-socratic-thinker-linux-amd64
mcp-server-socratic-thinker-darwin-arm64-<X.Y.Z>
mcp-server-socratic-thinker-darwin-arm64
mcp-server-socratic-thinker-windows-amd64-<X.Y.Z>.exe
mcp-server-socratic-thinker-windows-amd64.exe
SHA256SUMS-<X.Y.Z>
SHA256SUMS
install.sh
install.ps1
```

### Local end-to-end smoke (optional but recommended)

```sh
make build-all VERSION=v9.9.9
ls -lh dist/
./dist/mcp-server-socratic-thinker-linux-amd64 --version
go version -m dist/mcp-server-socratic-thinker-linux-amd64 | grep '^	build	CGO_ENABLED=0$'
```

Expected: prints `mcp-server-socratic-thinker version 9.9.9`, the
`build CGO_ENABLED=0` line appears, and all three binaries exist.

## Rollout and Rollback

### Rollout

1. Implement the plan on a feature branch (e.g. `ci/harden-release-pipeline`).
2. Push the branch; confirm the `go` and `go-native` jobs pass on a
   non-tag ref.
3. Push the `v1.2.3-rc1` malformed tag to confirm the regex guard,
   then delete the tag (see "Tag validation negative test" above).
4. Merge to `main` via PR review.
5. Push the real release tag. Do not push the tag before the merge;
   the release job depends on the merged workflow file.
6. Verify the GitHub Release asset list matches the expected manifest
   above.
7. Smoke-test one of the installers manually:
   * POSIX: `curl -fsSL https://github.com/maccavelli/mcp-server-socratic-thinker/releases/latest/download/install.sh | sh`
     should install into `~/.local/bin/mcp-server-socratic-thinker`
     and print `mcp-server-socratic-thinker <VER> installed to <dir>`.
   * Windows: `irm https://github.com/maccavelli/mcp-server-socratic-thinker/releases/latest/download/install.ps1 | iex`
     should install into `%LOCALAPPDATA%\Programs\socratic-thinker`.

### Rollback

If a regression is found after merging to `main` but before a tag push:

```sh
git revert <merge-commit>
git push origin main
```

If a regression is found after a tag push and release publication:

1. Delete the GitHub Release via the web UI or
   `gh release delete vX.Y.Z --yes`.
2. Delete the remote tag: `git push origin :refs/tags/vX.Y.Z`.
3. Revert or fix-forward on `main`; re-tag once CI is green.

The new workflow has no behavior that mutates state outside the
release tag's artifacts (no caches, no environments, no deploy
keys), so rollback is limited to GitHub Release + tag removal plus
a code revert. The legacy 72-line `ci.yml` is recoverable from `git
log` at any time.
