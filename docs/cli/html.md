# tobari html

`tobari html` generates an HTML coverage report. The output format depends on the input:

- **tobari.json**: Generates an interactive single-file HTML report with per-test coverage visualization, overlap analysis, and summary statistics.
- **coverprofile**: Generates a standard HTML report using `go tool cover`.

## Usage

```bash
tobari html [-o cover.html] [-b binary | -s sources.tar.gz] <tobari.json-or-coverprofile>
```

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-o` | `cover.html` | Output HTML file path |
| `-b` | - | Path to tobari-built binary with embedded sources |
| `-s` | - | Path to tar.gz archive of extracted sources |

`-b` and `-s` are mutually exclusive. If neither is specified, source files are read directly from the local filesystem using the paths in the input file.

## Interactive HTML Report (tobari.json input)

When given a `tobari.json` file, the generated HTML is a fully self-contained single file with no external dependencies. It automatically adapts to dark mode based on the OS/browser setting (`prefers-color-scheme: dark`). It consists of three tabs.

### Coverage Tab

The main view for visualizing per-test coverage on source code.

**Left Panel: Test Selection Tree**

- Displays test names as a hierarchical tree split by `/` (e.g., `TestDecoder/hello/world`)
- Checkboxes for selecting/deselecting individual tests
- Checking a parent node selects/deselects all children
- Test name filter (substring search)
- Select All / Deselect All buttons
- Tree is initially collapsed; click to expand
- Summary showing selected test count and coverage percentage

**Right Panel: Source Code Viewer**

- File selection dropdown with per-file coverage percentage
- Source code display with line numbers
- Line-level color coding:
  - Green: covered (executed by at least one selected test)
  - Red: uncovered (instrumented but not executed by any selected test)
  - No color: not instrumented
- Dynamic merge that updates in real time as test selection changes

**Compare Mode**

Selecting a test pair from the Overlap Analysis tab enters compare mode.

- Four-color coding:
  - Green: covered by both tests
  - Blue: covered by Test A only
  - Orange: covered by Test B only
  - Red: covered by neither test (instrumented line)
- Color legend banner displayed above the source code
- Diff navigation:
  - Prev / Next buttons to jump between diff lines (A-only or B-only)
  - Cross-file navigation (automatically switches to the next file when diffs in the current file are exhausted)
  - Position indicator (e.g., `3 / 20`)
  - Per-file diff count badges (click to jump to the first diff in that file)
  - Current diff line highlighted with a blue left border
- Exit Compare button to return to normal mode

### Overlap Analysis Tab

A view for analyzing coverage overlap between tests.

**Coverage Overlap Matrix**

- Heatmap visualizing overlap rates between top-level tests (grouped by name before `/`)
- Cell colors represent match rates:
  - Red: 90-100%
  - Orange: 70-90%
  - Yellow: 50-70%
  - Yellow-green: 30-50%
  - Gray: below 30%
- Hover on a cell to show test names and overlap rate in a tooltip
- Click a cell to enter compare mode in the Coverage tab

**Overlap Rankings**

- All test pairs listed by overlap rate in descending order
- Filters:
  - Test name search (substring match against Test A or Test B)
  - Match rate range (Min% / Max%)
  - Result count display (e.g., `123 / 4851 pairs`)
- Click a row to enter compare mode in the Coverage tab

**How Overlap Rate is Calculated**

Overlap rate is computed using Jaccard similarity. Each test's coverage is represented as a set of signatures (file position + covered/uncovered status). The overlap rate between two tests is `|common signatures| / |union of all signatures|`. Subtest coverage is merged into their top-level parent test.

### Summary Tab

A view displaying overall statistics.

**Overall Statistics**

- Total Coverage (coverage percentage across all tests)
- Tests (number of tests)
- Files (number of files)
- Lines Covered (covered lines / instrumented lines)

**Per-File Coverage**

- Instrumented lines, covered lines, and coverage percentage per file
- Sorted by coverage percentage ascending
- Includes a coverage bar

**Per-Test Coverage (top-level)**

- Covered lines, total instrumented lines, and coverage percentage per top-level test
- Sorted by coverage percentage descending
- Includes a coverage bar

## Standard HTML Report (coverprofile input)

When given a coverprofile file (starting with `mode:`), generates a standard HTML coverage report using `go tool cover -html`. This is the same output as running `go tool cover -html=profile.cover` directly.
