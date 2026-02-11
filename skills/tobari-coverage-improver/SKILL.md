---
name: tobari-coverage-improver
description: Analyze test coverage data from tobari.toon and help improve test coverage incrementally. Use when the user has run go test with tobari enabled (e.g., `GOFLAGS="$(tobari flags)" go test ./...`) and wants to improve test coverage or increase code coverage percentage. Triggers on phrases like "improve coverage", "increase coverage", "coverage improvement", or "add more tests".
---

# Improve Test Coverage

This skill analyzes test coverage data from tobari and helps improve test coverage incrementally.

## Prerequisites

Ensure you have run tests with tobari enabled:

```bash
GOFLAGS="$(tobari flags)" go test ./...
```

## How to Read Coverage Data

1. Check if `TOBARI_COVERDIR` environment variable is set:
   ```bash
   echo $TOBARI_COVERDIR
   ```
2. If set, read `$TOBARI_COVERDIR/tobari/tobari.toon`
3. If not set or empty, read `./tobari/tobari.toon` from current directory

## TOON Format Parsing

The tobari.toon file uses Token-Oriented Object Notation format:

```
TestName[N]{FileName,StartLine,StartCol,EndLine,EndCol,StatementCount,Count}:
	/path/to/file.go,7,24,9,2,1,4
	/path/to/file.go,11,29,12,22,1,0
```

- Top-level key is the test name (e.g., `Add` corresponds to `TestAdd` function)
- `[N]` indicates the number of entries
- Each indented line (starting with tab) is a coverage entry with comma-separated values:
  - FileName: source file path
  - StartLine, StartCol: start position
  - EndLine, EndCol: end position
  - StatementCount: number of statements in this block
  - Count: execution count (0 = not covered)

## Calculate Current Coverage

1. Collect all unique coverage entries across all tests:
   - Key = `FileName:StartLine:StartCol:EndLine:EndCol`
   - Track: StatementCount, MaxCount (highest Count across all tests)

2. Calculate coverage rate:
   ```
   TotalStatements = sum of all StatementCount values
   CoveredStatements = sum of StatementCount where MaxCount > 0
   CoverageRate = CoveredStatements / TotalStatements * 100
   ```

3. Report to user:
   ```
   Current coverage: XX.X% (Y of Z statements covered)
   ```

## Improve Coverage Incrementally

### Set Target and Improve

1. Set target: CurrentCoverage + 5% (capped at 100%)

2. Find uncovered code blocks (where Count = 0 for all tests):
   - Read the source file at those locations
   - Understand what code path leads to that block
   - Determine what test input would exercise that code

3. Implement improvements:
   - Add new test cases or modify existing ones
   - Follow existing test patterns in the codebase
   - Use table-driven tests when appropriate for Go

4. Run tests with tobari to verify coverage improved:
   ```bash
   GOFLAGS="$(tobari flags)" go test ./...
   ```

5. Re-read tobari.toon and calculate new coverage

### Report and Confirm

After each improvement cycle:

1. Show the user:
   - Previous coverage rate
   - New coverage rate
   - What was added/changed

2. Use AskUserQuestion:
   - Question: "Coverage improved from X% to Y%. How would you like to proceed?"
   - Options:
     - "Continue improving (+5% more)"
     - "Skip confirmations and continue to Z%" (where Z is a target like 80%, 90%, 100%)
     - "Stop here"

3. If user selects "Skip confirmations":
   - Ask for target percentage if not specified
   - Continue improving without confirmation until target is reached or no more improvements possible

4. If user selects "Continue", repeat from "Set Target and Improve"

## Important Guidelines

- Focus on meaningful coverage that tests actual behavior, not just line hits
- Consider edge cases: error paths, boundary conditions, nil/empty inputs
- When adding tests, follow existing patterns in the codebase
- Prefer table-driven tests for Go code when testing multiple inputs
- Don't add tests just to hit lines - ensure tests verify correct behavior
- If a code path is intentionally unreachable (dead code), suggest removing it instead of adding tests
