---
name: tobari-duplicated-tests-remover
description: Analyze test coverage data from tobari.toon to find and remove duplicate test cases that cover nearly identical code paths. Use when the user has run go test with tobari enabled (e.g., `GOFLAGS="$(tobari flags)" go test ./...`) and wants to find redundant tests. Triggers on phrases like "find duplicate tests", "remove redundant tests", "duplicated tests", or "test overlap".
---

# Remove Duplicated Tests

This skill analyzes test coverage data from tobari to find and remove duplicate test cases that cover nearly identical code paths.

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

## Detect Duplicate Test Cases

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
   - Re-run tests with tobari to verify:
     ```bash
     GOFLAGS="$(tobari flags)" go test ./...
     ```

4. If no duplicates found, inform the user that all tests have unique coverage patterns

## Guidelines

- Before removing tests, ensure they don't have different assertions even if coverage is similar
- Consider if tests document different use cases or edge cases
- Table-driven tests with multiple cases may show as duplicates - review the test logic before removal
