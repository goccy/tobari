# Overlay Templates

This directory contains Go source templates that tobari injects into standard library packages via the overlay mechanism.

- `runtime.go.tmpl` - Injected into `runtime` package
- `testing.go.tmpl` - Injected into `testing` package
- `testdeps.go.tmpl` - Injected into `testing/internal/testdeps` package

## Import Constraints

Packages imported by these templates must already exist in the dependency graph of the build target. If a template imports a package that is not in the dependency graph, the build will fail at link time with an unresolved symbol error.

### Why

tobari does not add `-overlay` to Go's build flags. Instead, when the `compile` tool is invoked via `-toolexec`, tobari checks if the package being compiled is an overlay target (via `overlay.TargetPackages()`). If so, it renders the overlay on-the-fly for that single package (via `overlay.RenderPackage()`), replacing source files and adding a new `tobari.go` file. This means Go's build system is unaware of the additional files and their imports. As a result:

1. **Compile time**: tobari adds missing imports to the compile-phase `importcfg` using archive paths obtained by `utils.GoListExportMap` (`go list -export -json`, see `internal/utils/go.go`). The compiler only needs type information (export data) from these archives, so fingerprint mismatches do not occur.

2. **Link time**: Go's build system generates a **separate** `importcfg` for linking, based on its own dependency analysis of the original source files. Only packages that Go recognizes as dependencies are included. If a template imports a package that Go does not know about, it will be missing from the link `importcfg`, causing a link error.

### Current Dependencies

**`runtime.go.tmpl`** imports:
- `internal/coverage/rtcov` - Already a dependency of `runtime`
- `unsafe` - No archive file needed

Since `runtime.go.tmpl` is the only template applied during `go run`/`go build`, and its imports are already runtime dependencies, there are no issues for non-test builds.

**`testing.go.tmpl`** imports:
- `unsafe` - No archive file needed

**`testdeps.go.tmpl`** imports:
- `encoding/json` - Dependency via `internal/coverage/cfile` (with `-cover`) and `internal/fuzz`
- `fmt` - Common transitive dependency of `testing`
- `os` - Common transitive dependency of `testing`
- `path/filepath` - Dependency via `internal/coverage/cfile` (with `-cover`)
- `unsafe` - No archive file needed
- `internal/coverage` - Dependency via `internal/coverage/cfile` (with `-cover`)
- `internal/coverage/encodemeta` - Dependency via `internal/coverage/cfile` (with `-cover`)
- `internal/coverage/rtcov` - Already a dependency of `runtime`
- `internal/coverage/slicewriter` - Dependency via `internal/coverage/cfile` (with `-cover`)

`testing.go.tmpl` and `testdeps.go.tmpl` are only applied during `go test`, where the testing framework (`testing/internal/testdeps`) and its transitive dependencies (including `internal/fuzz` and `internal/coverage/cfile`) are always present.

### Adding New Imports

When modifying templates, ensure that any new import is already a transitive dependency of the build target:

- For `runtime.go.tmpl`: Must be a dependency of `runtime` itself
- For `testing.go.tmpl` / `testdeps.go.tmpl`: Must be in the dependency graph of `go test -cover` (verify with `go list -deps -test -cover`)

If a required package is not in the dependency graph, a fundamental design change is needed (e.g., adding `-overlay` to `tobari flags` output so that Go's build system can see the template files and their imports).
