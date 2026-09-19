package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goccy/tobari"
	"github.com/goccy/tobari/internal/flags"
	"github.com/google/go-cmp/cmp"
)

// passedBlocksOnlyTestReport is a report of one program with a single file
// holding two blocks, of which the named test passed the first. Without
// passedBlocksOnly the second block is listed with a zero count.
func passedBlocksOnlyTestReport(name string, passedBlocksOnly bool) *tobari.CoverReport {
	return passedBlocksOnlyTestReportOf("/src/main.go", name, passedBlocksOnly)
}

func passedBlocksOnlyTestReportOf(file, name string, passedBlocksOnly bool) *tobari.CoverReport {
	report := &tobari.CoverReport{
		Metadata: tobari.CoverReportMetadata{
			Files: []string{file},
			Entry: []string{"FileName", "StartLine", "StartCol", "EndLine", "EndCol", "StatementCount"},
			All:   [][]int{{0, 3, 24, 5, 2, 1}, {0, 7, 13, 9, 2, 1}},
		},
		Counts: []*tobari.CoverReportCount{
			{Name: name, Coverprofile: [][]int{{0, 3}}, PassedBlocksOnly: passedBlocksOnly},
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

	merge := func(t *testing.T, reports ...*tobari.CoverReport) *tobari.CoverReport {
		t.Helper()
		dir := t.TempDir()
		args := []string{"-o", filepath.Join(dir, "merged.json")}
		for i, r := range reports {
			args = append(args, write(t, dir, fmt.Sprintf("%d.json", i), r))
		}
		c := &CLI{stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
		if err := c.runMergeJSONCmd(context.Background(), args); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(args[1])
		if err != nil {
			t.Fatal(err)
		}
		merged, err := parseTobariJSON(data)
		if err != nil {
			t.Fatal(err)
		}
		return merged
	}
	flagsByName := func(r *tobari.CoverReport) map[string]bool {
		m := make(map[string]bool, len(r.Counts))
		for _, c := range r.Counts {
			m[c.Name] = c.PassedBlocksOnly
		}
		return m
	}

	// Same program: the counts keep their own flag and the single source
	// stays implicit, so the merged file has the same shape as before.
	t.Run("same program", func(t *testing.T) {
		merged := merge(t, passedBlocksOnlyTestReport("TestA", true), passedBlocksOnlyTestReport("TestB", false))
		if got, want := flagsByName(merged), (map[string]bool{"TestA": true, "TestB": false}); !cmp.Equal(got, want) {
			t.Errorf("passedBlocksOnly by test = %v, want %v", got, want)
		}
		if merged.Metadata.Sources != nil {
			t.Errorf("a merge of one program must keep the implicit single source, got %v", merged.Metadata.Sources)
		}
		for _, c := range merged.Counts {
			if c.Source != 0 {
				t.Errorf("%s: source = %d, want 0", c.Name, c.Source)
			}
		}
	})

	// Different programs: each count keeps pointing at the files of its own
	// program, so "all instrumented blocks" of a passedBlocksOnly count does
	// not grow with the other program's blocks.
	t.Run("different programs", func(t *testing.T) {
		merged := merge(t,
			passedBlocksOnlyTestReportOf("/src/svc1/main.go", "TestS1", true),
			passedBlocksOnlyTestReportOf("/src/svc2/main.go", "TestS2", false),
		)
		if got, want := merged.Metadata.Files, []string{"/src/svc1/main.go", "/src/svc2/main.go"}; !cmp.Equal(got, want) {
			t.Fatalf("files = %v, want %v", got, want)
		}
		if got, want := merged.Metadata.Sources, [][]int{{0}, {1}}; !cmp.Equal(got, want) {
			t.Fatalf("sources = %v, want %v", got, want)
		}
		wantSource := map[string]int{"TestS1": 0, "TestS2": 1}
		for _, c := range merged.Counts {
			if c.Source != wantSource[c.Name] {
				t.Errorf("%s: source = %d, want %d", c.Name, c.Source, wantSource[c.Name])
			}
		}
		if got, want := flagsByName(merged), (map[string]bool{"TestS1": true, "TestS2": false}); !cmp.Equal(got, want) {
			t.Errorf("passedBlocksOnly by test = %v, want %v", got, want)
		}
		if got, want := merged.SourceFiles(0), []int{0}; !cmp.Equal(got, want) {
			t.Errorf("SourceFiles(0) = %v, want %v", got, want)
		}

		// Merging again with a third report of the first program must not
		// add a source: sources are identified by their file set.
		again := merge(t, merged, passedBlocksOnlyTestReportOf("/src/svc1/main.go", "TestS1b", true))
		if got, want := again.Metadata.Sources, [][]int{{0}, {1}}; !cmp.Equal(got, want) {
			t.Fatalf("sources after re-merge = %v, want %v", got, want)
		}
		for _, c := range again.Counts {
			if want := map[string]int{"TestS1": 0, "TestS1b": 0, "TestS2": 1}[c.Name]; c.Source != want {
				t.Errorf("%s: source = %d, want %d", c.Name, c.Source, want)
			}
		}
	})
}

// A passedBlocksOnly test gets no instrumented-line set of its own: the page
// derives its denominator from the instrumented lines of the test's source.
// Handing it the passed lines as the instrumented set would show the test as
// 100%.
func TestBuildTobarifmtData_PassedBlocksOnly(t *testing.T) {
	report, err := tobari.MergeCoverReports([]*tobari.CoverReport{
		passedBlocksOnlyTestReportOf("/src/svc1/main.go", "TestS1", true),
		passedBlocksOnlyTestReportOf("/src/svc2/main.go", "TestS2", false),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, fileIndexMap := buildFileIndexFromReport(report)
	data := buildTobarifmtData(report, expandCoverReport(report), nil, fileIndexMap)

	if got, want := data.Sources, [][]int{{0}, {1}}; !cmp.Equal(got, want) {
		t.Errorf("sources = %v, want %v", got, want)
	}
	tests := make(map[string]*tobarifmtTest, len(data.Tests))
	for _, tt := range data.Tests {
		tests[tt.Name] = tt
	}
	s1, s2 := tests["TestS1"], tests["TestS2"]
	if s1 == nil || s2 == nil {
		t.Fatalf("tests = %v", data.Tests)
	}
	if s1.Instr != nil || s1.Source != 0 || len(s1.Coverage[0]) != 3 {
		t.Errorf("passedBlocksOnly test: instr=%v source=%d coverage=%v", s1.Instr, s1.Source, s1.Coverage)
	}
	if len(s2.Instr[1]) != 6 || s2.Source != 1 || len(s2.Coverage[1]) != 3 {
		t.Errorf("default test: instr=%v source=%d coverage=%v", s2.Instr, s2.Source, s2.Coverage)
	}
	// Only the test that records the lines it could have passed defines the
	// CoverWithName scope.
	if got, want := data.InstrLinesScoped, (map[int][]int{1: {7, 8, 9, 3, 4, 5}}); len(got) != 1 || len(got[1]) != 6 {
		t.Errorf("instrLinesScoped = %v, want the 6 lines of file 1 only (%v)", got, want)
	}
	if len(data.InstrLinesAll[0]) != 6 || len(data.InstrLinesAll[1]) != 6 {
		t.Errorf("instrLinesAll = %v, want 6 lines per file", data.InstrLinesAll)
	}
}
