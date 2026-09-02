---
status: complete
date: 2026-09-02
associated-madr: 0002-MADR-remove-the-misleading-sanitizepath-helper.md
decision-makers: Socratic Thinker maintainers
---

# Implement the removal of `SanitizePath`

Associated MADR: [0002-MADR-remove-the-misleading-sanitizepath-helper.md](0002-MADR-remove-the-misleading-sanitizepath-helper.md)

<!-- markdownlint-disable MD013 MD024 -->

## Goal

`Go (windows/amd64)` passes, and the repository contains no exported helper
claiming to remove path traversal while doing neither containment nor correct
segment handling.

## Scope

**In scope.** `SanitizePath` and its doc comment in
`internal/metrics/store.go`; the single assertion in
`internal/metrics/store_test.go`; unused imports left behind.

**Out of scope.** Every other symbol in `internal/metrics`, including
`FormatBytes` and `defaultDBPath`; the MagicTools reaper failure, which is a
separate defect in a separate repository; anything in mcplib PLAN 0005 or 0006.

## Verified baseline

Record before changing anything:

    git rev-parse --short=12 HEAD
    git status --short
    grep -rn "SanitizePath" --include="*.go" .

The search must return exactly four hits, all in two files: the doc comment
(`internal/metrics/store.go:169`), the declaration (`:170`), and two references
inside the one test assertion (`internal/metrics/store_test.go:89` and `:90`,
the condition and its error message). **If it returns a production caller,
stop.** That would make this a live path-traversal issue
rather than dead code, and the MADR's decision would need revisiting before any
deletion.

## Implementation Steps

1. Confirm the baseline search returns no production caller.
2. Delete the `SanitizePath` function and its doc comment from
   `internal/metrics/store.go:169-171`.
3. Delete the assertion at `internal/metrics/store_test.go:88-90`. Leave the
   surrounding `FormatBytes` assertions and the rest of
   `TestStore_TickerAndHelpers` intact.
4. Remove `strings` from `store.go`'s imports if nothing else uses it; keep
   `path/filepath` if `defaultDBPath` or another function still needs it. Let
   the compiler decide, do not guess.
5. Run `gofmt` and per-file `golint` on both changed files.

## Verification

    grep -rn "SanitizePath" --include="*.go" .    # expect no output
    gofmt -l internal/metrics
    golint internal/metrics/store.go internal/metrics/store_test.go
    go build ./...
    go vet ./...
    go test ./internal/metrics
    go test ./...
    make lint

`go build ./...` succeeding is the proof that no caller existed: had one
remained, deletion would fail to compile. Record that explicitly rather than
asserting it from the search alone.

## Acceptance criteria

1. `grep -rn "SanitizePath" --include="*.go" .` returns nothing.
2. `go build ./...` succeeds, demonstrating there were no callers.
3. `go test ./internal/metrics` passes on ubuntu-24.04, macos-15 and
   windows-2025.
4. `TestStore_TickerAndHelpers` still exists and still asserts `FormatBytes`.
5. `make lint` reports 0 issues; per-file `golint` is clean.
6. No other exported symbol was removed or renamed.

## Rollout and Rollback

**Rollout.** One commit in this repository. No release, tag or dependency
change. Do not push without explicit authorization in the same turn.

**Rollback.** Revert the single commit. Do not "roll back" by re-adding the
helper with an OS-agnostic test: per the MADR that is strictly worse than the
current state, because it would make CI assert that a broken sanitizer works.

**Interaction with mcplib PLAN 0005 Gate G2.** Immutable releases are already
enabled on this repository and the publish job is gated behind the test job, so
a tag pushed while Windows is red would create a release that cannot be
completed or fixed in place. Land this, confirm all three runners are green,
then tag `v1.1.0`.

## Execution Record

Populate during execution.

| Step | Status | Commit | Evidence | Deviation |
|---|---|---|---|---|
| Baseline caller search | complete | (this commit) | 4 hits in exactly two files at HEAD 067a43820851, matching the plan: `store.go:169-170` and `store_test.go:89-90`. No production caller, so the deletion decision stands | none |
| Deletion | complete | (this commit) | `SanitizePath` and its doc comment removed from `internal/metrics/store.go`; the single assertion removed from `store_test.go`; `strings` import dropped because the compiler reported it unused, while `path/filepath` was kept since `defaultDBPath` and `Open` still use it (`store.go:58`, `:69`) | the deletion left a trailing blank line that `gofmt -l` flagged; fixed with `gofmt -w` rather than left, and re-verified clean |
| Verification suite | complete | (this commit) | All ten acceptance checks pass: 0 `SanitizePath` references; gofmt; per-file golint; `go build ./...`; `go vet ./...`; `go test ./internal/metrics`; `go test ./...`; `make lint`; `FormatBytes` coverage retained; `TestStore_TickerAndHelpers` still present. `go build ./...` succeeding is the positive proof that no caller existed | none |
| Windows CI green | complete | 509945f | Run 33692196259 on 509945f: `Go (windows/amd64)` **success**, alongside darwin/arm64 and linux/amd64. This is the first green Windows run for this repository since the hardened pipeline added the job, and it confirms the separator behaviour that could only be observed on a Windows runner | none |
