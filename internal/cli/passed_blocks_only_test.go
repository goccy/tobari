package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goccy/tobari"
	"github.com/goccy/tobari/internal/flags"
)

func passedBlocksOnlyTestReport(name string, passedBlocksOnly bool) *tobari.CoverReport {
	report := &tobari.CoverReport{
		Metadata: tobari.CoverReportMetadata{
			Files:            []string{"/src/main.go"},
			Entry:            []string{"FileName", "StartLine", "StartCol", "EndLine", "EndCol", "StatementCount"},
			All:              [][]int{{0, 3, 24, 5, 2, 1}, {0, 7, 13, 9, 2, 1}},
			PassedBlocksOnly: passedBlocksOnly,
		},
		Counts: []*tobari.CoverReportCount{
			{Name: name, Coverprofile: [][]int{{0, 3}}},
		},
		AllCounts: []int{3, 0},
	}
	if !passedBlocksOnly {
		report.Counts[0].Coverprofile = append(report.Counts[0].Coverprofile, []int{1, 0})
	}
	return report
}

func TestRunFlagsCmd_PassedBlocksOnly(t *testing.T) {
	tobariBin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	t.Run("forwarded to toolexec", func(t *testing.T) {
		var stdout bytes.Buffer
		c := &CLI{stdout: &stdout, stderr: &bytes.Buffer{}}
		if err := c.runFlagsCmd(context.Background(), tobariBin, []string{"-passed-blocks-only"}); err != nil {
			t.Fatal(err)
		}
		if want := "'-toolexec=" + tobariBin + " --passed-blocks-only'"; !strings.Contains(stdout.String(), want) {
			t.Errorf("flags output = %q, want to contain %q", stdout.String(), want)
		}
	})

	t.Run("absent by default", func(t *testing.T) {
		var stdout bytes.Buffer
		c := &CLI{stdout: &stdout, stderr: &bytes.Buffer{}}
		if err := c.runFlagsCmd(context.Background(), tobariBin, nil); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(stdout.String(), "passed-blocks-only") {
			t.Errorf("flags output = %q, must not mention the option", stdout.String())
		}
	})

	t.Run("rejected with exclude-analysis", func(t *testing.T) {
		c := &CLI{stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
		err := c.runFlagsCmd(context.Background(), tobariBin, []string{"-passed-blocks-only", "-exclude-analysis=example.com/x"})
		if !errors.Is(err, flags.ErrExcludeAnalysisWithPassedBlocksOnly) {
			t.Fatalf("err = %v, want ErrExcludeAnalysisWithPassedBlocksOnly", err)
		}
	})
}

func TestCLI_Run_ToolexecRejectsPassedBlocksOnlyWithExcludeAnalysis(t *testing.T) {
	c := &CLI{stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
	err := c.Run(context.Background(), []string{
		"tobari", "--passed-blocks-only", "--exclude-analysis=example.com/x", "/path/to/compile", "-V=full",
	})
	if !errors.Is(err, flags.ErrExcludeAnalysisWithPassedBlocksOnly) {
		t.Fatalf("err = %v, want ErrExcludeAnalysisWithPassedBlocksOnly", err)
	}
}

func TestRunMergeJSONCmd_PassedBlocksOnly(t *testing.T) {
	write := func(t *testing.T, dir, name string, report *tobari.CoverReport) string {
		t.Helper()
		data, err := json.Marshal(report)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}

	t.Run("kept when every input agrees", func(t *testing.T) {
		dir := t.TempDir()
		a := write(t, dir, "a.json", passedBlocksOnlyTestReport("TestA", true))
		b := write(t, dir, "b.json", passedBlocksOnlyTestReport("TestB", true))
		out := filepath.Join(dir, "merged.json")

		c := &CLI{stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
		if err := c.runMergeJSONCmd(context.Background(), []string{"-o", out, a, b}); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		merged, err := parseTobariJSON(data)
		if err != nil {
			t.Fatal(err)
		}
		if !merged.Metadata.PassedBlocksOnly {
			t.Error("merged report lost metadata.passedBlocksOnly")
		}
	})

	t.Run("mixed inputs are rejected", func(t *testing.T) {
		dir := t.TempDir()
		a := write(t, dir, "a.json", passedBlocksOnlyTestReport("TestA", true))
		b := write(t, dir, "b.json", passedBlocksOnlyTestReport("TestB", false))

		c := &CLI{stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
		err := c.runMergeJSONCmd(context.Background(), []string{"-o", filepath.Join(dir, "merged.json"), a, b})
		if !errors.Is(err, tobari.ErrMixedPassedBlocksOnly) {
			t.Fatalf("err = %v, want ErrMixedPassedBlocksOnly", err)
		}
	})
}

// A passedBlocksOnly report gives tests no instrumented-line set of their own:
// the page is told so and derives denominators from InstrLinesAll. Handing it
// the passed lines as the instrumented set would show every test as 100%.
func TestBuildTobarifmtData_PassedBlocksOnly(t *testing.T) {
	for _, passedBlocksOnly := range []bool{false, true} {
		report := passedBlocksOnlyTestReport("TestA", passedBlocksOnly)
		_, fileIndexMap := buildFileIndexFromReport(report)
		data := buildTobarifmtData(report, expandCoverReport(report), nil, fileIndexMap)

		if data.PassedBlocksOnly != passedBlocksOnly {
			t.Errorf("passedBlocksOnly=%v: data.PassedBlocksOnly = %v", passedBlocksOnly, data.PassedBlocksOnly)
		}
		if len(data.Tests) != 1 {
			t.Fatalf("passedBlocksOnly=%v: expected 1 test, got %d", passedBlocksOnly, len(data.Tests))
		}
		if got := len(data.Tests[0].Coverage[0]); got != 3 {
			t.Errorf("passedBlocksOnly=%v: covered lines = %d, want 3", passedBlocksOnly, got)
		}
		if got := len(data.InstrLinesAll[0]); got != 6 {
			t.Errorf("passedBlocksOnly=%v: all instrumented lines = %d, want 6", passedBlocksOnly, got)
		}
		if hasOwnDenominator := data.Tests[0].Instr != nil || data.InstrLinesScoped != nil; hasOwnDenominator == passedBlocksOnly {
			t.Errorf("passedBlocksOnly=%v: Instr=%v InstrLinesScoped=%v", passedBlocksOnly, data.Tests[0].Instr, data.InstrLinesScoped)
		}
	}
}
