package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goccy/tobari"
)

// createTestGoFile creates a simple Go source file and returns its absolute path.
func createTestGoFile(t *testing.T, dir string) string {
	t.Helper()
	src := filepath.Join(dir, "main.go")
	content := `package main

func add(a, b int) int {
	return a + b
}

func main() {
	add(1, 2)
}
`
	if err := os.WriteFile(src, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return src
}

// createCoverprofile creates a coverprofile file referencing the given source file.
func createCoverprofile(t *testing.T, dir, srcPath string) string {
	t.Helper()
	profile := filepath.Join(dir, "cover.out")
	content := fmt.Sprintf("mode: set\n%s:3.24,5.2 1 1\n%s:7.13,9.2 1 1\n", srcPath, srcPath)
	if err := os.WriteFile(profile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return profile
}

// createTobariJSON creates a tobari.json file in the new compact format
// referencing the given source file.
func createTobariJSON(t *testing.T, dir, srcPath string) string {
	t.Helper()
	report := tobari.CoverReport{
		Metadata: tobari.CoverReportMetadata{
			Files: []string{srcPath},
			Entry: []string{"file", "startLine", "startCol", "endLine", "endCol", "stmts"},
			All: [][]int{
				{0, 3, 24, 5, 2, 1}, // block 0: add function
				{0, 7, 13, 9, 2, 1}, // block 1: main function
			},
		},
		Counts: []*tobari.CoverReportCount{
			{Name: "TestAdd", Coverprofile: [][]int{{0, 3}}},
			{Name: "TestMain", Coverprofile: [][]int{{0, 2}, {1, 1}}},
		},
	}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	jsonPath := filepath.Join(dir, "tobari.json")
	if err := os.WriteFile(jsonPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return jsonPath
}

// createSourceTarGz creates a tar.gz archive containing the given source file
// with a tar entry name matching filepath.ToSlash(srcPath) (same as tobari extract).
func createSourceTarGz(t *testing.T, dir, srcPath string) string {
	t.Helper()
	content, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatal(err)
	}

	tarGzPath := filepath.Join(dir, "sources.tar.gz")
	f, err := os.Create(tarGzPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	gw := gzip.NewWriter(f)
	tw := tar.NewWriter(gw)

	if err := tw.WriteHeader(&tar.Header{
		Name: filepath.ToSlash(srcPath),
		Mode: 0o600,
		Size: int64(len(content)),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	return tarGzPath
}

func TestParseTobariJSON_CompactFormat(t *testing.T) {
	data := []byte(`{
		"metadata": {
			"files": ["/path/to/main.go"],
			"entry": ["file", "startLine", "startCol", "endLine", "endCol", "stmts"],
			"all": [[0, 3, 24, 5, 2, 1], [0, 7, 13, 9, 2, 1]]
		},
		"counts": [
			{"name": "TestA", "coverprofile": [[0, 3]]},
			{"name": "TestB", "coverprofile": [[0, 2], [1, 1]]}
		]
	}`)
	entriesMap, err := parseTobariJSON(data)
	if err != nil {
		t.Fatalf("parseTobariJSON() error = %v", err)
	}
	if len(entriesMap) != 2 {
		t.Fatalf("expected 2 tests, got %d", len(entriesMap))
	}
	testA := entriesMap["TestA"]
	if len(testA) != 1 {
		t.Fatalf("TestA: expected 1 entry, got %d", len(testA))
	}
	if testA[0].FileName != "/path/to/main.go" || testA[0].Count != 3 {
		t.Errorf("TestA[0]: FileName=%q, Count=%d", testA[0].FileName, testA[0].Count)
	}
	if testA[0].Start.Line != 3 || testA[0].Start.Column != 24 {
		t.Errorf("TestA[0] Start: Line=%d, Col=%d", testA[0].Start.Line, testA[0].Start.Column)
	}
	testB := entriesMap["TestB"]
	if len(testB) != 2 {
		t.Fatalf("TestB: expected 2 entries, got %d", len(testB))
	}
}

func TestParseTobariJSON_LegacyFormat(t *testing.T) {
	data := []byte(`{
		"TestA": [
			{"FileName": "/path/to/main.go", "Start": {"Line": 3, "Column": 24}, "End": {"Line": 5, "Column": 2}, "StatementCount": 1, "Count": 3}
		],
		"TestB": [
			{"FileName": "/path/to/main.go", "Start": {"Line": 7, "Column": 13}, "End": {"Line": 9, "Column": 2}, "StatementCount": 1, "Count": 1}
		]
	}`)
	entriesMap, err := parseTobariJSON(data)
	if err != nil {
		t.Fatalf("parseTobariJSON() error = %v", err)
	}
	if len(entriesMap) != 2 {
		t.Fatalf("expected 2 tests, got %d", len(entriesMap))
	}
	testA := entriesMap["TestA"]
	if len(testA) != 1 {
		t.Fatalf("TestA: expected 1 entry, got %d", len(testA))
	}
	if testA[0].FileName != "/path/to/main.go" || testA[0].Count != 3 {
		t.Errorf("TestA[0]: FileName=%q, Count=%d", testA[0].FileName, testA[0].Count)
	}
}

func TestRunHTMLCmd_Coverprofile(t *testing.T) {
	srcDir := t.TempDir()
	srcPath := createTestGoFile(t, srcDir)
	profilePath := createCoverprofile(t, srcDir, srcPath)
	outputPath := filepath.Join(t.TempDir(), "out.html")

	var stdout, stderr bytes.Buffer
	c := &CLI{stdout: &stdout, stderr: &stderr}

	if err := c.runHTMLCmd(context.Background(), []string{"-o", outputPath, profilePath}); err != nil {
		t.Fatalf("runHTMLCmd() error = %v\nstderr: %s", err, stderr.String())
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("failed to read output HTML: %v", err)
	}
	if !strings.Contains(string(data), "<!DOCTYPE html>") {
		t.Errorf("output HTML does not contain <!DOCTYPE html>")
	}
	if !strings.Contains(stdout.String(), "HTML coverage report written to") {
		t.Errorf("stdout = %q, want to contain success message", stdout.String())
	}
}

func TestRunHTMLCmd_TobariJSON(t *testing.T) {
	srcDir := t.TempDir()
	srcPath := createTestGoFile(t, srcDir)
	jsonPath := createTobariJSON(t, srcDir, srcPath)
	outputPath := filepath.Join(t.TempDir(), "out.html")

	var stdout, stderr bytes.Buffer
	c := &CLI{stdout: &stdout, stderr: &stderr}

	if err := c.runHTMLCmd(context.Background(), []string{"-o", outputPath, jsonPath}); err != nil {
		t.Fatalf("runHTMLCmd() error = %v\nstderr: %s", err, stderr.String())
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
	if !strings.Contains(stdout.String(), "HTML coverage report written to") {
		t.Errorf("stdout = %q, want to contain success message", stdout.String())
	}
}

func TestRunHTMLCmd_TobariJSON_MergesCounts(t *testing.T) {
	srcDir := t.TempDir()
	srcPath := createTestGoFile(t, srcDir)

	// Create tobari.json with 3 blocks: block 2 is uncovered by any test.
	report := tobari.CoverReport{
		Metadata: tobari.CoverReportMetadata{
			Files: []string{srcPath},
			Entry: []string{"file", "startLine", "startCol", "endLine", "endCol", "stmts"},
			All: [][]int{
				{0, 3, 24, 5, 2, 1},   // block 0: add function
				{0, 7, 13, 9, 2, 1},   // block 1: main function
				{0, 11, 1, 13, 2, 1},  // block 2: uncovered function (no test covers it)
			},
		},
		Counts: []*tobari.CoverReportCount{
			{Name: "TestAdd", Coverprofile: [][]int{{0, 3}}},
			{Name: "TestMain", Coverprofile: [][]int{{0, 2}, {1, 1}}},
		},
	}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}

	profile, err := tobariJSONToCoverprofile(data)
	if err != nil {
		t.Fatalf("tobariJSONToCoverprofile() error = %v", err)
	}

	lines := strings.Split(strings.TrimSpace(profile), "\n")

	// TestAdd has Count=3, TestMain has Count=2 for block 0 (3.24,5.2).
	// They should be summed to 5.
	foundMerged := false
	for _, line := range lines {
		if strings.Contains(line, "3.24,5.2") {
			if !strings.HasSuffix(line, " 1 5") {
				t.Errorf("expected merged count 5, got line: %s", line)
			}
			foundMerged = true
			break
		}
	}
	if !foundMerged {
		t.Errorf("block 3.24,5.2 not found in merged coverprofile:\n%s", profile)
	}

	// Block 2 (11.1,13.2) is in metadata.all but no test covers it.
	// It should still appear with count=0.
	foundUncovered := false
	for _, line := range lines {
		if strings.Contains(line, "11.1,13.2") {
			if !strings.HasSuffix(line, " 1 0") {
				t.Errorf("expected uncovered block with count 0, got line: %s", line)
			}
			foundUncovered = true
			break
		}
	}
	if !foundUncovered {
		t.Errorf("uncovered block 11.1,13.2 not found in coverprofile:\n%s", profile)
	}

	// Total: mode line + 3 blocks = 4 lines.
	if len(lines) != 4 {
		t.Errorf("expected 4 lines (mode + 3 blocks), got %d:\n%s", len(lines), profile)
	}
}

func TestRunHTMLCmd_CoverprofileWithSources(t *testing.T) {
	srcDir := t.TempDir()
	srcPath := createTestGoFile(t, srcDir)
	tarGzPath := createSourceTarGz(t, srcDir, srcPath)
	profilePath := createCoverprofile(t, srcDir, srcPath)
	outputPath := filepath.Join(t.TempDir(), "out.html")

	var stdout, stderr bytes.Buffer
	c := &CLI{stdout: &stdout, stderr: &stderr}

	if err := c.runHTMLCmd(context.Background(), []string{"-o", outputPath, "-s", tarGzPath, profilePath}); err != nil {
		t.Fatalf("runHTMLCmd() error = %v\nstderr: %s", err, stderr.String())
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("failed to read output HTML: %v", err)
	}
	if !strings.Contains(string(data), "<!DOCTYPE html>") {
		t.Errorf("output HTML does not contain <!DOCTYPE html>")
	}
}

func TestRunHTMLCmd_TobariJSONWithSources(t *testing.T) {
	srcDir := t.TempDir()
	srcPath := createTestGoFile(t, srcDir)
	tarGzPath := createSourceTarGz(t, srcDir, srcPath)
	jsonPath := createTobariJSON(t, srcDir, srcPath)
	outputPath := filepath.Join(t.TempDir(), "out.html")

	var stdout, stderr bytes.Buffer
	c := &CLI{stdout: &stdout, stderr: &stderr}

	if err := c.runHTMLCmd(context.Background(), []string{"-o", outputPath, "-s", tarGzPath, jsonPath}); err != nil {
		t.Fatalf("runHTMLCmd() error = %v\nstderr: %s", err, stderr.String())
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("failed to read output HTML: %v", err)
	}
	if !strings.Contains(string(data), "func add") {
		t.Error("output HTML does not contain source code content")
	}
}

func TestRunHTMLCmd_DefaultOutput(t *testing.T) {
	srcDir := t.TempDir()
	srcPath := createTestGoFile(t, srcDir)
	profilePath := createCoverprofile(t, srcDir, srcPath)

	// Run in a temp dir so the default coverage.html is created there.
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

	if err := c.runHTMLCmd(context.Background(), []string{profilePath}); err != nil {
		t.Fatalf("runHTMLCmd() error = %v\nstderr: %s", err, stderr.String())
	}

	defaultOutput := filepath.Join(outDir, "cover.html")
	if _, err := os.Stat(defaultOutput); err != nil {
		t.Errorf("default output file not created: %v", err)
	}
	if !strings.Contains(stdout.String(), "cover.html") {
		t.Errorf("stdout = %q, want to contain 'cover.html'", stdout.String())
	}
}

func TestRunHTMLCmd_Errors(t *testing.T) {
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
			args:       []string{}, // will be set in the test body
			wantErrMsg: "failed to parse tobari.json",
		},
		{
			name:       "nonexistent binary",
			args:       []string{"-b", "/nonexistent/binary", "input"},
			wantErrMsg: "failed to extract sources from binary",
		},
		{
			name:       "nonexistent tar.gz",
			args:       []string{"-s", "/nonexistent/sources.tar.gz", "input"},
			wantErrMsg: "failed to extract tar.gz",
		},
		{
			name:       "invalid tar.gz file",
			args:       []string{}, // will be set in the test body
			wantErrMsg: "failed to extract tar.gz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := tt.args

			switch tt.name {
			case "invalid json input":
				tmpDir := t.TempDir()
				invalidJSON := filepath.Join(tmpDir, "bad.json")
				if err := os.WriteFile(invalidJSON, []byte("{invalid json}"), 0o644); err != nil {
					t.Fatal(err)
				}
				args = []string{invalidJSON}

			case "nonexistent binary":
				tmpDir := t.TempDir()
				srcPath := createTestGoFile(t, tmpDir)
				inputPath := createCoverprofile(t, tmpDir, srcPath)
				args = []string{"-b", "/nonexistent/binary", inputPath}

			case "nonexistent tar.gz":
				tmpDir := t.TempDir()
				srcPath := createTestGoFile(t, tmpDir)
				inputPath := createCoverprofile(t, tmpDir, srcPath)
				args = []string{"-s", "/nonexistent/sources.tar.gz", inputPath}

			case "invalid tar.gz file":
				tmpDir := t.TempDir()
				srcPath := createTestGoFile(t, tmpDir)
				inputPath := createCoverprofile(t, tmpDir, srcPath)
				invalidTarGz := filepath.Join(tmpDir, "bad.tar.gz")
				if err := os.WriteFile(invalidTarGz, []byte("not a tar.gz"), 0o644); err != nil {
					t.Fatal(err)
				}
				args = []string{"-s", invalidTarGz, inputPath}
			}

			var stdout, stderr bytes.Buffer
			c := &CLI{stdout: &stdout, stderr: &stderr}

			err := c.runHTMLCmd(context.Background(), args)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErrMsg) {
				t.Errorf("error = %q, want to contain %q", err.Error(), tt.wantErrMsg)
			}
		})
	}
}
