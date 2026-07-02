# Selective Dependency Analysis for Whole-Program RTA

Status: planned (instrumentation landed on this branch; analysis changes not yet implemented)

## Problem

When the main package (or testmain) is instrumented, `CreateMainDeps`
(`internal/cover/deps.go`) runs a whole-program analysis:

1. `go list -deps -json .` for dependency metadata (served to
   `packages.Load` via the custom GOPACKAGESDRIVER — no compilation)
2. `packages.Load` with `NeedSyntax | NeedDeps | ...` — parses and
   type-checks **every** dependency package from source
3. `ssautil.AllPackages` + `prog.Build()` — builds SSA bodies for
   **every** package
4. `rta.Analyze` — whole-program RTA call graph
5. Aggregation of cover-function → cover-function edges into suppDeps

Steps 2 and 3 are unfiltered: they pay full cost for every third-party
dependency, including huge generated code that never contributes any
edge to the result.

### Benchmark evidence

Bench project: a `main` + one coverage-target package `sqlcheck` that
calls `github.com/goccy/go-googlesql` (`ParseStatement` etc.). Built
with `CGO_ENABLED=0 GOOS=linux GOARCH=386` so that the
`//go:build !amd64 && !arm64` pure-Go path of `googlesqlwasm2go`
(~90MB of generated Go across p0–p10) is selected.

Wall time (cold build cache):

| condition | time |
|---|---|
| `go build` without tobari | 73s |
| `go build` with tobari | 118s |

Analysis breakdown (`TOBARI_PROFILE` instrumentation):

| phase | time | scale |
|---|---|---|
| go list | 0.7s | 122 dep packages |
| packages.Load | 15.4s | all 122 parsed + type-checked from source |
| ssa build | 14.9s | 313,639 SSA functions |
| rta.Analyze | 5.7–8.6s | 204,856 call-graph nodes, 1 root |
| suppdeps agg | 0.1s | 1 cover pkg |

The CPU profile is dominated by GC and allocation pressure from SSA
construction (`gcBgMarkWorker` 33%, `mallocgc` 29%, `madvise` 18%;
`ssa.(*builder).buildFunction` cum 21%).

**The entire ~40s analysis produced `sqlcheck.init -> []` — zero
dependency edges.** The 90MB dependency contributed nothing to the
answer. This is the case to optimize.

## Key insight: pruning can be decided without opening the package

The suppDeps result only contains cover→cover edges. A pruned
third-party package P can affect the result only if control can
*re-enter* cover code through P. There are exactly two mechanisms:

- **(a) Static re-entry.** P (transitively) imports a cover package and
  calls it. Decidable from the `go list -deps -json` import graph alone
  — no parsing of P needed. Transitivity automatically covers bridge
  chains (P → helper lib → cover pkg): every package that can
  statically reach cover code imports it transitively.

- **(b) Higher-order re-entry.** Cover-adjacent code passes P a value
  that can carry cover code: a func value, a value of a method-bearing
  type declared in a cover package, or an interface value whose dynamic
  type may be one of those (note: `error` is an interface). P cannot
  fabricate such values on its own — it does not import the cover
  package. Every such value must be handed over at a call site in code
  that *does* see cover code, i.e. inside the analyzed region. So the
  hand-off is fully observable on the caller side.

Therefore, define the analyzed set:

```
S = {main} ∪ coverPkgs ∪ { P : P transitively imports a cover pkg }
```

computed from go list metadata (free), then verify on S's SSA that no
"non-inert" value escapes into any call whose callee is outside S.
If nothing escapes, every package outside S is safe to prune: load its
types from export data, skip source parsing and SSA entirely.

If a non-inert value does escape into package Q, grow S (fixed-point
iteration: add Q, analyze it, repeat). This degenerates to today's
whole-program analysis only when values genuinely flow everywhere.

Precision note: escapes into packages that the dependency walk already
prunes (`isRuntimePackage`, `isHTTPPackage`, `isGRPCGoPackage` in
`analyzeMainFuncDepsRecursive`) cannot produce reported edges anyway,
so such escapes (e.g. `http.HandleFunc(coverHandler)`) do not force
those packages into S. This keeps the fast path viable for typical
HTTP/gRPC servers without changing current result semantics.

Out-of-scope re-entry channels (reflection, //go:linkname) are already
outside RTA's soundness guarantees today, so pruning does not regress
them.

## Export data acquisition: the actionID trap

Pruned packages still need type information (to type-check S). The
source is export data (.a files). Measurements:

- At cover(main) time, `$WORK/bNNN/` contains only `pkgcfg.txt` and
  `coveroutfiles.txt` — **importcfg does not exist yet** (verified).
  So export paths cannot be taken from importcfg.
- All of main's dependencies are guaranteed already compiled *with
  tobari's GOFLAGS* when cover(main) runs: Go's action graph orders all
  dep compile actions before main's compile action (cover is part of
  it).
- `go list -export -deps` **with identical flags** (same GOFLAGS
  including `-cover -toolexec=...`, same GOOS/GOARCH/CGO_ENABLED):
  **1s**, all export paths returned from the build cache.
- `go list -export -deps` **with mismatched flags** (different
  actionID): **69s** — go list recompiles every package (including the
  90MB) to produce export data. This is the worst case and must never
  happen.

Design consequences:

1. `packages.Load`'s internal `go list` is already fully bypassed by
   the custom GOPACKAGESDRIVER, so there is no code path where
   packages.Load searches with the wrong actionID. tobari itself runs
   `go list -export` for the pruned set with the *preserved* build
   environment and serves the resulting paths via the driver response
   `ExportFile` field.
2. The export query must **keep** GOFLAGS (the opposite of
   `FilterGOFLAGSEnvs`, which strips it to avoid recursive toolexec).
   On cache hits — the guaranteed case — no toolexec process is ever
   spawned. If some package misses despite matching flags, the rebuild
   shares actionIDs (and thus artifacts) with the outer build, so the
   work is not duplicated, only fronted.
3. Never include the main package (`.`) in the export query: main is
   not compiled yet; querying it would trigger its build → cover(main)
   → `CreateMainDeps` recursion. Query only the pruned dependency
   packages, which are guaranteed cached.
4. Defense in depth: set a guard env var (e.g. `TOBARI_IN_ANALYSIS=1`)
   during the analysis so that a nested `CreateMainDeps` becomes a
   no-op instead of recursing.
5. Flag/env reconstruction (`-tags`, `-coverpkg`, `-trimpath`, GOOS,
   GOARCH, CGO_ENABLED, ...) is correctness-critical and must be locked
   down by tests: a silent mismatch converts the 1s path into the 69s
   path.
6. Instrumented (cover-target) packages must never be loaded from
   export data — by construction they are in S and always parsed from
   source, so this holds automatically.

## Implementation plan

### Phase 1 — instrumentation (done, this branch)

`TOBARI_PROFILE=<dir>` enables CPU profiling and per-phase timings
(go list / packages.Load / ssa build / rta.Analyze / suppdeps agg) plus
scale counters (dep packages, SSA functions, RTA roots/nodes, cover
pkgs, dep edges) in `CreateMainDeps`. Dormant when unset.

### Phase 2 — import-graph pruning + boundary escape analysis

1. Compute S from the go list JSON already in hand (import-graph
   reachability to coverPkgSet; include test variants `pkg [pkg.test]`).
2. Run `go list -export` for packages outside S with the preserved
   build env; extend the driver response: source metadata for S,
   `ExportFile` for pruned packages.
3. `packages.Load` with roots = S so `NeedSyntax` applies only to S;
   pruned deps load types via export data.
4. Build SSA only for S (imported packages become bodyless SSA
   packages created from export types).
5. Boundary escape check on S's SSA: taint cover-origin func values and
   values of method-bearing cover-declared types; if any tainted value
   flows into a call whose callee is outside S (excluding the existing
   runtime/net-http/grpc skip list), the pruning is unsafe.
   - v1 policy: any unsafe escape → fall back to today's full analysis
     (zero precision risk).
   - v2 policy: fixed-point growth of S with per-package inclusion.
6. RTA over the partial program; suppDeps aggregation unchanged.

### Phase 3 — validation

- suppDeps output must be byte-identical before/after on all existing
  testdata (`thirdparty*`, `crossdeps`, `crosspkg`, `generic`,
  `channel`, `http`, `grpc`, `initorder`, `initfunc`, `synctest`,
  `embedcode`, `covername`, `toolchain`, `notobari`).
- New testdata:
  - inert-args case (third-party API called with plain data — prune
    fires; modeled on the go-googlesql bench),
  - callback-bridge case (cover func value passed through a third-party
    package back into another cover package — v1 must fall back and
    still produce the bridge edge),
  - third-party importing a cover package (rule (a) keeps it in S).
- Benchmark before/after on the go-googlesql GOARCH=386 bench with
  `TOBARI_PROFILE`.

## Open questions / risks

- `ssautil` behavior with mixed syntax/export-data packages (bodyless
  `ssa.Package` from export types) — verify RTA handles bodyless callees
  as opaque without error.
- `go list -export` runs while the outer `go build` is still in flight;
  the build cache is designed for concurrent access, but verify no lock
  contention or ID instability in practice.
- Consistency of `types` objects between export-data-loaded packages
  and source-checked packages within one `packages.Load` (should be
  handled by the driver contract, but verify identity of named types
  across the boundary).
- Escape check granularity for interface arguments: v1 may
  over-approximate (any interface-typed argument constructed from
  cover-origin values), which only costs fallback frequency, never
  precision.

## Expected outcome

For the bench: S = {main, sqlcheck}, export query ~1s, so the analysis
drops from ~40s to an estimated 2–3s with an identical suppDeps result.
Projects whose cover code passes only plain data into third-party APIs
get the full speedup; projects with genuine cross-package callbacks
fall back to today's behavior (v1) or pay only for the packages the
values actually reach (v2).
