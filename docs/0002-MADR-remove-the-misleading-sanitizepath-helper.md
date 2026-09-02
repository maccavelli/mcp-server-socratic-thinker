---
status: accepted
date: 2026-09-02
decision-makers: Socratic Thinker maintainers
consulted: CI run 33672094670 (windows-2025)
informed: Socratic Thinker contributors
---

# Remove `SanitizePath` rather than fix the test that pins its broken behaviour

<!-- markdownlint-disable MD013 MD024 -->

> Paired with [0002-PLAN-remove-the-misleading-sanitizepath-helper.md](0002-PLAN-remove-the-misleading-sanitizepath-helper.md).

## Context and Problem Statement

`Go (windows/amd64)` fails. CI run
[33672094670](https://github.com/maccavelli/mcp-server-socratic-thinker/actions/runs/33672094670):

```text
--- FAIL: TestStore_TickerAndHelpers (0.03s)
    store_test.go:90: SanitizePath = a\b
FAIL  github.com/maccavelli/mcp-server-socratic-thinker/internal/metrics
```

The assertion (`internal/metrics/store_test.go:88-90`) is:

```go
if SanitizePath("a/../b") != "a/b" { ... }
```

and the implementation (`internal/metrics/store.go:169-171`) is:

```go
// SanitizePath removes any path traversal attempts.
func SanitizePath(p string) string {
    return filepath.Clean(strings.ReplaceAll(p, "..", ""))
}
```

`ReplaceAll` turns `a/../b` into `a//b`; `filepath.Clean` then yields `a/b` on
Unix and `a\b` on Windows, because `filepath` is separator-aware by design. The
test hard-codes a forward slash, so it can only ever pass off Windows.

### Why this surfaced now

Nothing regressed. Before the CI hardening merged in `f62221a`, this
repository's workflow had `validate`, `build` and `release` jobs and **no
Windows job at all**. The hardened pipeline added `Go (windows/amd64)`, which
ran this assertion on Windows for the first time. The bug is as old as the
helper; the pipeline simply started looking.

### The function is more wrong than the test

Fixing only the separator would leave a helper whose documented purpose it does
not fulfil:

* **It corrupts legitimate names.** `strings.ReplaceAll(p, "..", "")` deletes
  that sequence anywhere, not only as a path segment, so `report..final.json`
  becomes `reportfinal.json`.
* **It confines nothing.** `SanitizePath("/etc/passwd")` returns
  `/etc/passwd` unchanged, and `SanitizePath("C:\\Windows\\System32")` is
  likewise untouched. A function documented as removing "any path traversal
  attempts" does not constrain the result to any root.
* **Its only cure for traversal is string surgery**, applied before
  `filepath.Clean` rather than after resolution, which is not how containment is
  established.

### It has no production callers

A repository-wide search finds four references in two files: the declaration,
its doc comment, and the single test assertion, which names it twice. Nothing in the shipped code path calls it. There is therefore
**no live vulnerability**, and no compatibility obligation: this is an exported
symbol nothing uses.

That combination is the actual hazard. The next person needing to sanitize a
path will find an exported, plausibly named helper with a reassuring comment,
and use it.

## Decision Drivers

* The Windows job must pass without asserting behaviour the code should not have.
* A security-named helper must either do what its name says or not exist.
* Prefer deleting an unused trap to preserving it for symmetry.
* Do not write a containment API speculatively, with no caller to define the
  root it must enforce.

## Considered Options

* Delete `SanitizePath` and the assertion that pins its behaviour
* Make the test OS-agnostic with `filepath.ToSlash` and keep the function
* Reimplement `SanitizePath` as real containment now
* Skip the assertion on Windows

## Decision Outcome

Chosen option: "Delete `SanitizePath` and the assertion that pins its
behaviour", because the helper is unused, does not do what it says, and its
only consumer is a test asserting the broken result — so deletion removes the
CI failure and the trap in one change, with no caller to migrate.

If path containment is needed later, it will be written against a real caller
with a real root to enforce, using `filepath.Clean` plus an explicit
prefix check or `os.Root`, and named for what it actually guarantees.

### Consequences

* Good, because the Windows job passes without encoding a platform assumption
  into an assertion.
* Good, because an exported helper that invites misuse is gone before it
  acquires a caller.
* Good, because the change is provably safe: no production code references it.
* Neutral, because `FormatBytes` coverage in the same test is untouched.
* Bad, because a future need for sanitization starts from nothing rather than
  from an existing helper. That is intended: starting from this helper would be
  starting from a defect.

### Confirmation

* `grep -rn "SanitizePath" --include="*.go" .` returns nothing.
* `go test ./internal/metrics` passes on ubuntu-24.04, macos-15 and
  windows-2025.
* `go build ./...` succeeds, proving no caller existed.
* The rest of `TestStore_TickerAndHelpers` still runs and still asserts
  `FormatBytes`.

## Pros and Cons of the Options

### Delete the helper and its assertion

* Good, because it removes the failure and the trap together.
* Good, because it is verifiable: the build proves there were no callers.
* Bad, because it removes a symbol someone may have intended to use.

### Make the test OS-agnostic and keep the function

* Good, because it is the smallest possible diff and turns CI green.
* Bad, because it makes CI assert that a broken sanitizer is correct, which is
  worse than the current state: today the helper is merely unused, afterwards
  it is unused **and** covered by a passing test that implies it works.
* Bad, because the doc comment's claim stays false.

### Reimplement it as real containment now

* Good, because the fleet will plausibly need containment eventually.
* Bad, because there is no caller, so there is no root to confine to and no way
  to test the guarantee against a real use.
* Bad, because a security primitive written speculatively and never exercised
  is the same category of hazard this record is removing.

### Skip the assertion on Windows

* Bad, because it hides a platform-specific defect on the platform it affects,
  which is what the hardened pipeline was added to stop.

## More Information

* CI runs [33672094670](https://github.com/maccavelli/mcp-server-socratic-thinker/actions/runs/33672094670)
  (`067a438`) and 33668788622 (`3e4499b`); both fail identically.
* `internal/metrics/store.go:169-171` and `internal/metrics/store_test.go:88-90`.
* Commit `f62221a`, which added the `Go (windows/amd64)` job, and
  [MADR 0001](0001-MADR-port-magictools-ci-cd-pipeline.md), which introduced the
  hardened pipeline.
* [`path/filepath.Clean`](https://pkg.go.dev/path/filepath#Clean), whose result
  is separator-dependent by design.
* [`os.Root`](https://pkg.go.dev/os#Root), the Go 1.24+ primitive for confining
  filesystem access beneath a directory.
