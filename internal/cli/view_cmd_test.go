package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunViewCmd_TobariJSON(t *testing.T) {
	srcDir := t.TempDir()
	srcPath := createTestGoFile(t, srcDir)
	jsonPath := createTobariJSON(t, srcDir, srcPath)
	outputPath := filepath.Join(t.TempDir(), "out.html")

	var stdout, stderr bytes.Buffer
	c := &CLI{stdout: &stdout, stderr: &stderr}

	if err := c.runViewCmd(context.Background(), []string{"-o", outputPath, jsonPath}); err != nil {
		t.Fatalf("runViewCmd() error = %v\nstderr: %s", err, stderr.String())
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("failed to read output HTML: %v", err)
	}
	html := string(data)

	if !strings.Contains(html, "<!DOCTYPE html>") {
		t.Error("output HTML does not contain <!DOCTYPE html>")
	}
	if !strings.Contains(html, "Tobari Coverage Report") {
		t.Error("output HTML does not contain title")
	}
	if !strings.Contains(html, "TestAdd") {
		t.Error("output HTML does not contain test name TestAdd")
	}
	if !strings.Contains(html, "TestMain") {
		t.Error("output HTML does not contain test name TestMain")
	}
	if !strings.Contains(stdout.String(), "Interactive coverage report written to") {
		t.Errorf("stdout = %q, want to contain success message", stdout.String())
	}
}

func TestRunViewCmd_TobariJSONWithSources(t *testing.T) {
	srcDir := t.TempDir()
	srcPath := createTestGoFile(t, srcDir)
	tarGzPath := createSourceTarGz(t, srcDir, srcPath)
	jsonPath := createTobariJSON(t, srcDir, srcPath)
	outputPath := filepath.Join(t.TempDir(), "out.html")

	var stdout, stderr bytes.Buffer
	c := &CLI{stdout: &stdout, stderr: &stderr}

	if err := c.runViewCmd(context.Background(), []string{"-o", outputPath, "-s", tarGzPath, jsonPath}); err != nil {
		t.Fatalf("runViewCmd() error = %v\nstderr: %s", err, stderr.String())
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("failed to read output HTML: %v", err)
	}
	if !strings.Contains(string(data), "func add") {
		t.Error("output HTML does not contain source code content")
	}
}

func TestRunViewCmd_DefaultOutput(t *testing.T) {
	srcDir := t.TempDir()
	srcPath := createTestGoFile(t, srcDir)
	jsonPath := createTobariJSON(t, srcDir, srcPath)

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	outDir := t.TempDir()
	if err := os.Chdir(outDir); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	var stdout, stderr bytes.Buffer
	c := &CLI{stdout: &stdout, stderr: &stderr}

	if err := c.runViewCmd(context.Background(), []string{jsonPath}); err != nil {
		t.Fatalf("runViewCmd() error = %v\nstderr: %s", err, stderr.String())
	}

	defaultOutput := filepath.Join(outDir, "coverage-view.html")
	if _, err := os.Stat(defaultOutput); err != nil {
		t.Errorf("default output file not created: %v", err)
	}
	if !strings.Contains(stdout.String(), "coverage-view.html") {
		t.Errorf("stdout = %q, want to contain 'coverage-view.html'", stdout.String())
	}
}

func TestRunViewCmd_RejectsCoverprofile(t *testing.T) {
	srcDir := t.TempDir()
	srcPath := createTestGoFile(t, srcDir)
	profilePath := createCoverprofile(t, srcDir, srcPath)

	var stdout, stderr bytes.Buffer
	c := &CLI{stdout: &stdout, stderr: &stderr}

	err := c.runViewCmd(context.Background(), []string{profilePath})
	if err == nil {
		t.Fatal("expected error for coverprofile input, got nil")
	}
	if !strings.Contains(err.Error(), "tobari view requires tobari.json") {
		t.Errorf("error = %q, want to contain tobari.json requirement message", err.Error())
	}
}

func TestRunViewCmd_Errors(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantErrMsg string
	}{
		{
			name:       "missing input file",
			args:       []string{},
			wantErrMsg: "missing input file",
		},
		{
			name:       "mutually exclusive -b and -s",
			args:       []string{"-b", "bin", "-s", "src.tar.gz", "input"},
			wantErrMsg: "-b and -s flags are mutually exclusive",
		},
		{
			name:       "flags after input file",
			args:       []string{"input.json", "-o", "out.html"},
			wantErrMsg: "flags must be specified before the input file",
		},
		{
			name:       "nonexistent input file",
			args:       []string{"/nonexistent/path/input.json"},
			wantErrMsg: "failed to read input file",
		},
		{
			name:       "invalid json input",
			args:       nil,
			wantErrMsg: "failed to parse tobari.json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := tt.args

			if tt.name == "invalid json input" {
				tmpDir := t.TempDir()
				invalidJSON := filepath.Join(tmpDir, "bad.json")
				if err := os.WriteFile(invalidJSON, []byte("{invalid json}"), 0o644); err != nil {
					t.Fatal(err)
				}
				args = []string{invalidJSON}
			}

			var stdout, stderr bytes.Buffer
			c := &CLI{stdout: &stdout, stderr: &stderr}

			err := c.runViewCmd(context.Background(), args)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErrMsg) {
				t.Errorf("error = %q, want to contain %q", err.Error(), tt.wantErrMsg)
			}
		})
	}
}

func TestBuildTestTree(t *testing.T) {
	names := []string{
		"TestAdd",
		"TestDecoder",
		"TestDecoder/hello",
		"TestDecoder/world",
		"TestEncoder",
		"TestEncoder/a",
		"TestEncoder/b",
	}
	tree := buildTestTree(names)

	if len(tree) != 3 {
		t.Fatalf("expected 3 top-level nodes, got %d", len(tree))
	}

	// TestAdd
	if tree[0].Name != "TestAdd" || !tree[0].IsLeaf || len(tree[0].Children) != 0 {
		t.Errorf("TestAdd node: name=%s, isLeaf=%v, children=%d", tree[0].Name, tree[0].IsLeaf, len(tree[0].Children))
	}

	// TestDecoder
	if tree[1].Name != "TestDecoder" || !tree[1].IsLeaf || len(tree[1].Children) != 2 {
		t.Errorf("TestDecoder node: name=%s, isLeaf=%v, children=%d", tree[1].Name, tree[1].IsLeaf, len(tree[1].Children))
	}
	if tree[1].Children[0].Name != "hello" || tree[1].Children[0].FullName != "TestDecoder/hello" {
		t.Errorf("TestDecoder/hello: name=%s, fullName=%s", tree[1].Children[0].Name, tree[1].Children[0].FullName)
	}

	// TestEncoder
	if tree[2].Name != "TestEncoder" || !tree[2].IsLeaf || len(tree[2].Children) != 2 {
		t.Errorf("TestEncoder node: name=%s, isLeaf=%v, children=%d", tree[2].Name, tree[2].IsLeaf, len(tree[2].Children))
	}
}

func TestConvertToLineCoverage(t *testing.T) {
	entries := []tobariJSONEntry{
		{FileName: "a.go", Start: tobariEntryPos{Line: 3, Column: 1}, End: tobariEntryPos{Line: 5, Column: 2}, Count: 1},
		{FileName: "a.go", Start: tobariEntryPos{Line: 7, Column: 1}, End: tobariEntryPos{Line: 8, Column: 2}, Count: 0}, // not covered
		{FileName: "b.go", Start: tobariEntryPos{Line: 10, Column: 1}, End: tobariEntryPos{Line: 12, Column: 2}, Count: 2},
	}
	fileIndexMap := map[string]int{"a.go": 0, "b.go": 1}
	cov := convertToLineCoverage(entries, fileIndexMap)

	// a.go: lines 3,4,5 covered (count=1), lines 7,8 NOT covered (count=0)
	aLines := cov[0]
	if len(aLines) != 3 {
		t.Fatalf("a.go: expected 3 covered lines, got %d: %v", len(aLines), aLines)
	}

	// b.go: lines 10,11,12 covered
	bLines := cov[1]
	if len(bLines) != 3 {
		t.Fatalf("b.go: expected 3 covered lines, got %d: %v", len(bLines), bLines)
	}
}

func TestShortenFilePaths(t *testing.T) {
	paths := []string{
		"/Users/user/project/pkg/decode.go",
		"/Users/user/project/pkg/encode.go",
		"/Users/user/project/internal/scanner.go",
	}
	short := shortenFilePaths(paths)
	if len(short) != 3 {
		t.Fatalf("expected 3 paths, got %d", len(short))
	}
	// Common prefix is /Users/user/project/
	for _, s := range short {
		if strings.HasPrefix(s, "/Users/user/project/") {
			t.Errorf("path %q still has common prefix", s)
		}
	}
}

func TestComputeOverlaps(t *testing.T) {
	entriesMap := map[string][]tobariJSONEntry{
		"TestA": {
			{FileName: "a.go", Start: tobariEntryPos{Line: 1, Column: 1}, End: tobariEntryPos{Line: 3, Column: 2}, StatementCount: 1, Count: 1},
			{FileName: "a.go", Start: tobariEntryPos{Line: 5, Column: 1}, End: tobariEntryPos{Line: 7, Column: 2}, StatementCount: 1, Count: 1},
		},
		"TestB": {
			{FileName: "a.go", Start: tobariEntryPos{Line: 1, Column: 1}, End: tobariEntryPos{Line: 3, Column: 2}, StatementCount: 1, Count: 1},
			{FileName: "a.go", Start: tobariEntryPos{Line: 5, Column: 1}, End: tobariEntryPos{Line: 7, Column: 2}, StatementCount: 1, Count: 0},
		},
	}
	fileIndexMap := map[string]int{"a.go": 0}

	overlaps := computeOverlaps(entriesMap, fileIndexMap)
	if len(overlaps) != 1 {
		t.Fatalf("expected 1 overlap pair, got %d", len(overlaps))
	}
	pair := overlaps[0]
	if pair.TestA != "TestA" || pair.TestB != "TestB" {
		t.Errorf("pair: %s <-> %s", pair.TestA, pair.TestB)
	}
	// TestA sigs: (0:1:1:3:2:1), (0:5:1:7:2:1)
	// TestB sigs: (0:1:1:3:2:1), (0:5:1:7:2:0)
	// Common: (0:1:1:3:2:1) = 1
	// Total: 3 unique sigs
	if pair.Common != 1 {
		t.Errorf("expected common=1, got %d", pair.Common)
	}
	if pair.Total != 3 {
		t.Errorf("expected total=3, got %d", pair.Total)
	}
}
