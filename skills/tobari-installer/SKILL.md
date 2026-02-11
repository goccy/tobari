---
name: tobari-installer
description: Install the tobari CLI tool for scoped coverage measurement in Go. Use when the user wants to install tobari, set up tobari, or prepare their environment for coverage measurement. Triggers on phrases like "install tobari", "setup tobari", or "get started with tobari".
---

# Install Tobari

This skill helps install the tobari CLI tool for scoped coverage measurement in Go.

## Installation

Install the tobari CLI using the following command:

```bash
go install github.com/goccy/tobari/cmd/tobari@latest
```

## Verify Installation

After installation, verify that tobari is available:

```bash
tobari version
```

## Important Version Matching

The version of the tobari CLI tool and the tobari library used in your application must match.

For example, if you install `tobari@v0.2.1`, your `go.mod` should also require:
```
github.com/goccy/tobari v0.2.1
```

Version mismatch will cause fingerprint errors during linking.

## Basic Usage

After installation, you can run tests with tobari coverage:

```bash
GOFLAGS="$(tobari flags)" go test ./...
```

This will generate coverage data in `tobari/tobari.json` and `tobari/tobari.toon`.

### Specifying Output Directory

Use `TOBARI_COVERDIR` to specify where coverage data should be written:

```bash
TOBARI_COVERDIR=/path/to/output GOFLAGS="$(tobari flags)" go test ./...
```

## Next Steps

After running tests with tobari:
- Use `tobari-duplicated-tests-remover` skill to find and remove duplicate tests
- Use `tobari-coverage-improver` skill to improve test coverage incrementally
