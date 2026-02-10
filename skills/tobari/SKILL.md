---
name: tobari
description: Analyze test coverage data from tobari.toon and help improve test coverage. Use when the user has run go test with tobari enabled (e.g., `GOFLAGS="$(tobari flags)" go test ./...`) and wants to improve test coverage, find duplicate tests, or increase code coverage percentage. Triggers on phrases like "tobari", "improve coverage", "coverage analysis", "find duplicate tests", "increase test coverage", or after running go test with tobari flags.
---

# Improve Test Coverage

This skill analyzes test coverage data from tobari and helps improve test coverage incrementally.

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

## Step 1: Detect Duplicate Test Cases

Compare coverage entries between all test cases to find tests that cover nearly identical code paths.

### Calculate Match Rate

For each pair of tests (TestA, TestB):

1. Create a set of "coverage signatures" for each test:
   - Signature = `FileName:StartLine:StartCol:EndLine:EndCol:IsCovered`
   - IsCovered = 1 if Count > 0, else 0

2. Calculate match rate:
   ```
   CommonSignatures = signatures in both TestA and TestB with same IsCovered value
   AllSignatures = union of all signatures from both tests
   MatchRate = len(CommonSignatures) / len(AllSignatures) * 100
   ```

3. If MatchRate > 95%, these tests are considered duplicates

### Action for Duplicates

If duplicate test pairs are found:

1. List all test pairs with >95% match rate, showing:
   - Test names
   - Match percentage
   - Number of shared coverage entries

2. Use AskUserQuestion to ask the user:
   - Question: "The following test pairs cover almost identical code paths. Which test(s) would you like to remove?"
   - Options for each duplicate pair, plus "Keep all tests"

3. If user selects tests to remove:
   - Find the test file containing those tests
   - Delete the selected test functions
   - Re-run tests with tobari to verify

4. If no duplicates found or user chooses to keep all, proceed to Step 2

## Step 2: Improve Coverage Incrementally

### Calculate Current Coverage

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
