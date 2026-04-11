package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goccy/tobari"
)

func TestRunMergeCmd_MissingSubcommand(t *testing.T) {
	c := &CLI{stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
	err := c.runMergeCmd(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "missing subcommand") {
		t.Errorf("expected missing subcommand error, got: %v", err)
	}
}

func TestRunMergeCmd_UnknownSubcommand(t *testing.T) {
	c := &CLI{stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
	err := c.runMergeCmd(context.Background(), []string{"unknown"})
	if err == nil || !strings.Contains(err.Error(), "unknown merge subcommand") {
		t.Errorf("expected unknown subcommand error, got: %v", err)
	}
}

func TestRunMergeJSONCmd_NoInputs(t *testing.T) {
	c := &CLI{stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
	err := c.runMergeJSONCmd(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "missing input") {
		t.Errorf("expected missing input error, got: %v", err)
	}
}

func TestClassifyMergeArg(t *testing.T) {
	cases := []struct {
		in       string
		wantKind string
		wantBase string
		wantErr  bool
	}{
		{"./...", "pattern", ".", false},
		{"./foo/...", "pattern", "foo", false},
		{"pkg/...", "pattern", "pkg", false},
		{"/abs/path/...", "pattern", "/abs/path", false},
		{"a.json", "file", "", false},
		{"some/dir/tobari.json", "file", "", false},
		{"...", "", "", true},
		{"-o", "", "", true},
		{"/...", "", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			kind, base, err := classifyMergeArg(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got kind=%q base=%q", kind, base)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if kind != tc.wantKind {
				t.Errorf("kind = %q, want %q", kind, tc.wantKind)
			}
			if base != tc.wantBase {
				t.Errorf("base = %q, want %q", base, tc.wantBase)
			}
		})
	}
}

func TestRunMergeJSONCmd_PatternWalk(t *testing.T) {
	dir := t.TempDir()

	// Two valid tobari.json files under packages.
	writeJSONAt(t, filepath.Join(dir, "svc1", "tobari", "tobari.json"),
		minimalReport("TestSvc1", "/src/svc1/main.go"))
	writeJSONAt(t, filepath.Join(dir, "svc2", "tobari", "tobari.json"),
		minimalReport("TestSvc2", "/src/svc2/main.go"))

	// Noise that must NOT be picked up.
	if err := os.MkdirAll(filepath.Join(dir, "svc3"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "svc3", "notes.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	// tobari.json directly under a package, parent is not "tobari" dir.
	writeJSONAt(t, filepath.Join(dir, "svc4", "tobari.json"),
		minimalReport("TestSvc4", "/src/svc4/main.go"))
	// vendor/ should be skipped.
	writeJSONAt(t, filepath.Join(dir, "vendor", "x", "tobari", "tobari.json"),
		minimalReport("TestVendor", "/src/vendor/main.go"))
	// Dot-prefixed dir should be skipped.
	writeJSONAt(t, filepath.Join(dir, ".hidden", "tobari", "tobari.json"),
		minimalReport("TestHidden", "/src/hidden/main.go"))

	outputPath := filepath.Join(dir, "out.json")
	var stdout bytes.Buffer
	c := &CLI{stdout: &stdout, stderr: &bytes.Buffer{}}
	if err := c.runMergeJSONCmd(context.Background(), []string{
		"-o", outputPath,
		filepath.Join(dir, "..."),
	}); err != nil {
		t.Fatalf("runMergeJSONCmd() error = %v", err)
	}

	if !strings.Contains(stdout.String(), "Merged 2 reports") {
		t.Errorf("stdout = %q, want to contain 'Merged 2 reports'", stdout.String())
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}
	var merged tobari.CoverReport
	if err := json.Unmarshal(data, &merged); err != nil {
		t.Fatalf("failed to parse merged output: %v", err)
	}
	if len(merged.Counts) != 2 {
		t.Errorf("counts = %d, want 2 (svc1+svc2 only)", len(merged.Counts))
	}
	gotNames := map[string]bool{}
	for _, cnt := range merged.Counts {
		gotNames[cnt.Name] = true
	}
	for _, want := range []string{"TestSvc1", "TestSvc2"} {
		if !gotNames[want] {
			t.Errorf("missing test %q in merged result: %v", want, gotNames)
		}
	}
	for _, bad := range []string{"TestSvc4", "TestVendor", "TestHidden"} {
		if gotNames[bad] {
			t.Errorf("unexpected test %q in merged result", bad)
		}
	}
}

func TestRunMergeJSONCmd_PatternMixed(t *testing.T) {
	dir := t.TempDir()

	writeJSONAt(t, filepath.Join(dir, "a", "tobari", "tobari.json"),
		minimalReport("TestA", "/src/a/main.go"))
	writeJSONAt(t, filepath.Join(dir, "extra.json"),
		minimalReport("TestExtra", "/src/extra/main.go"))

	outputPath := filepath.Join(dir, "out.json")
	var stdout bytes.Buffer
	c := &CLI{stdout: &stdout, stderr: &bytes.Buffer{}}
	if err := c.runMergeJSONCmd(context.Background(), []string{
		"-o", outputPath,
		filepath.Join(dir, "a", "..."),
		filepath.Join(dir, "extra.json"),
	}); err != nil {
		t.Fatalf("runMergeJSONCmd() error = %v", err)
	}

	if !strings.Contains(stdout.String(), "Merged 2 reports") {
		t.Errorf("stdout = %q, want to contain 'Merged 2 reports'", stdout.String())
	}
}

func TestRunMergeJSONCmd_PatternDedup(t *testing.T) {
	dir := t.TempDir()

	writeJSONAt(t, filepath.Join(dir, "a", "b", "tobari", "tobari.json"),
		minimalReport("TestAB", "/src/ab/main.go"))

	outputPath := filepath.Join(dir, "out.json")
	var stdout bytes.Buffer
	c := &CLI{stdout: &stdout, stderr: &bytes.Buffer{}}
	if err := c.runMergeJSONCmd(context.Background(), []string{
		"-o", outputPath,
		filepath.Join(dir, "a", "..."),
		filepath.Join(dir, "a", "b", "..."),
	}); err != nil {
		t.Fatalf("runMergeJSONCmd() error = %v", err)
	}

	// The single file must not be counted twice.
	if !strings.Contains(stdout.String(), "Merged 1 reports") {
		t.Errorf("stdout = %q, want to contain 'Merged 1 reports' (dedup)", stdout.String())
	}
}

func TestRunMergeJSONCmd_PatternNoMatches(t *testing.T) {
	dir := t.TempDir()

	c := &CLI{stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
	err := c.runMergeJSONCmd(context.Background(), []string{
		"-o", filepath.Join(dir, "out.json"),
		filepath.Join(dir, "..."),
	})
	if err == nil || !strings.Contains(err.Error(), "no tobari/tobari.json files found") {
		t.Errorf("expected no-matches error, got: %v", err)
	}
}

func TestRunMergeJSONCmd_BareEllipsis(t *testing.T) {
	c := &CLI{stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
	err := c.runMergeJSONCmd(context.Background(), []string{"..."})
	if err == nil || !strings.Contains(err.Error(), "bare '...'") {
		t.Errorf("expected bare ellipsis error, got: %v", err)
	}
}

func TestRunMergeJSONCmd_NonexistentFile(t *testing.T) {
	c := &CLI{stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
	err := c.runMergeJSONCmd(context.Background(), []string{"nonexistent1.json", "nonexistent2.json"})
	if err == nil || !strings.Contains(err.Error(), "failed to read input file") {
		t.Errorf("expected file read error, got: %v", err)
	}
}

func TestRunMergeJSONCmd_SameSource(t *testing.T) {
	dir := t.TempDir()

	// Create two tobari.json files with same metadata (same server, different tests).
	report1 := tobari.CoverReport{
		Metadata: tobari.CoverReportMetadata{
			Files: []string{"/src/main.go"},
			Entry: []string{"FileName", "StartLine", "StartCol", "EndLine", "EndCol", "StatementCount"},
			All:   [][]int{{0, 3, 24, 5, 2, 1}, {0, 7, 13, 9, 2, 1}},
		},
		Counts: []*tobari.CoverReportCount{
			{Name: "TestA", Coverprofile: [][]int{{0, 3}}},
		},
		AllCounts: []int{3, 0},
	}
	report2 := tobari.CoverReport{
		Metadata: tobari.CoverReportMetadata{
			Files: []string{"/src/main.go"},
			Entry: []string{"FileName", "StartLine", "StartCol", "EndLine", "EndCol", "StatementCount"},
			All:   [][]int{{0, 3, 24, 5, 2, 1}, {0, 7, 13, 9, 2, 1}},
		},
		Counts: []*tobari.CoverReportCount{
			{Name: "TestB", Coverprofile: [][]int{{1, 2}}},
		},
		AllCounts: []int{0, 2},
	}

	writeJSON(t, dir, "a.json", report1)
	writeJSON(t, dir, "b.json", report2)

	outputPath := filepath.Join(dir, "merged.json")
	var stdout bytes.Buffer
	c := &CLI{stdout: &stdout, stderr: &bytes.Buffer{}}
	if err := c.runMergeJSONCmd(context.Background(), []string{
		"-o", outputPath,
		filepath.Join(dir, "a.json"),
		filepath.Join(dir, "b.json"),
	}); err != nil {
		t.Fatalf("runMergeJSONCmd() error = %v", err)
	}

	if !strings.Contains(stdout.String(), "Merged 2 reports") {
		t.Errorf("stdout = %q, want to contain success message", stdout.String())
	}

	// Parse and verify merged result.
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}
	var merged tobari.CoverReport
	if err := json.Unmarshal(data, &merged); err != nil {
		t.Fatalf("failed to parse merged output: %v", err)
	}
	if len(merged.Metadata.Files) != 1 || merged.Metadata.Files[0] != "/src/main.go" {
		t.Errorf("files = %v, want [/src/main.go]", merged.Metadata.Files)
	}
	if len(merged.Metadata.All) != 2 {
		t.Errorf("all blocks = %d, want 2", len(merged.Metadata.All))
	}
	if len(merged.Counts) != 2 {
		t.Errorf("counts = %d, want 2", len(merged.Counts))
	}
	if len(merged.AllCounts) != 2 {
		t.Fatalf("allcounts length = %d, want 2", len(merged.AllCounts))
	}
	if merged.AllCounts[0] != 3 || merged.AllCounts[1] != 2 {
		t.Errorf("allcounts = %v, want [3 2]", merged.AllCounts)
	}
}

func TestRunMergeJSONCmd_CrossSource(t *testing.T) {
	dir := t.TempDir()

	// Create two tobari.json files with different metadata (different servers).
	report1 := tobari.CoverReport{
		Metadata: tobari.CoverReportMetadata{
			Files: []string{"/src/server1/main.go"},
			Entry: []string{"FileName", "StartLine", "StartCol", "EndLine", "EndCol", "StatementCount"},
			All:   [][]int{{0, 10, 1, 20, 2, 3}},
		},
		Counts: []*tobari.CoverReportCount{
			{Name: "TestS1", Coverprofile: [][]int{{0, 5}}},
		},
		AllCounts: []int{5},
	}
	report2 := tobari.CoverReport{
		Metadata: tobari.CoverReportMetadata{
			Files: []string{"/src/server2/handler.go"},
			Entry: []string{"FileName", "StartLine", "StartCol", "EndLine", "EndCol", "StatementCount"},
			All:   [][]int{{0, 5, 1, 15, 2, 2}},
		},
		Counts: []*tobari.CoverReportCount{
			{Name: "TestS2", Coverprofile: [][]int{{0, 7}}},
		},
		AllCounts: []int{7},
	}

	writeJSON(t, dir, "s1.json", report1)
	writeJSON(t, dir, "s2.json", report2)

	outputPath := filepath.Join(dir, "merged.json")
	c := &CLI{stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
	if err := c.runMergeJSONCmd(context.Background(), []string{
		"-o", outputPath,
		filepath.Join(dir, "s1.json"),
		filepath.Join(dir, "s2.json"),
	}); err != nil {
		t.Fatalf("runMergeJSONCmd() error = %v", err)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}
	var merged tobari.CoverReport
	if err := json.Unmarshal(data, &merged); err != nil {
		t.Fatalf("failed to parse merged output: %v", err)
	}
	if len(merged.Metadata.Files) != 2 {
		t.Errorf("files = %d, want 2", len(merged.Metadata.Files))
	}
	if len(merged.Metadata.All) != 2 {
		t.Errorf("all blocks = %d, want 2", len(merged.Metadata.All))
	}
	if len(merged.Counts) != 2 {
		t.Errorf("counts = %d, want 2", len(merged.Counts))
	}
	// Files should be sorted.
	if merged.Metadata.Files[0] != "/src/server1/main.go" || merged.Metadata.Files[1] != "/src/server2/handler.go" {
		t.Errorf("files not sorted: %v", merged.Metadata.Files)
	}
}

func TestRunMergeSourceCmd_TooFewInputs(t *testing.T) {
	c := &CLI{stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
	err := c.runMergeSourceCmd(context.Background(), []string{"only-one.tar.gz"})
	if err == nil || !strings.Contains(err.Error(), "at least two input files") {
		t.Errorf("expected input count error, got: %v", err)
	}
}

func TestRunMergeSourceCmd_Success(t *testing.T) {
	dir := t.TempDir()

	// Create two tar.gz archives with different files.
	createTestTarGz(t, filepath.Join(dir, "a.tar.gz"), map[string]string{
		"/src/a.go": "package a\n",
	})
	createTestTarGz(t, filepath.Join(dir, "b.tar.gz"), map[string]string{
		"/src/b.go": "package b\n",
	})

	outputPath := filepath.Join(dir, "merged.tar.gz")
	var stdout bytes.Buffer
	c := &CLI{stdout: &stdout, stderr: &bytes.Buffer{}}
	if err := c.runMergeSourceCmd(context.Background(), []string{
		"-o", outputPath,
		filepath.Join(dir, "a.tar.gz"),
		filepath.Join(dir, "b.tar.gz"),
	}); err != nil {
		t.Fatalf("runMergeSourceCmd() error = %v", err)
	}

	if !strings.Contains(stdout.String(), "Merged 2 source archives") {
		t.Errorf("stdout = %q, want success message", stdout.String())
	}

	// Verify merged archive.
	files := readTarGz(t, outputPath)
	if len(files) != 2 {
		t.Fatalf("merged archive has %d files, want 2", len(files))
	}
	if files["/src/a.go"] != "package a\n" {
		t.Errorf("a.go content = %q", files["/src/a.go"])
	}
	if files["/src/b.go"] != "package b\n" {
		t.Errorf("b.go content = %q", files["/src/b.go"])
	}
}

func TestRunMergeSourceCmd_DuplicateSkip(t *testing.T) {
	dir := t.TempDir()

	// Create two identical tar.gz archives.
	createTestTarGz(t, filepath.Join(dir, "a.tar.gz"), map[string]string{
		"/src/main.go": "package main\n",
	})
	// Copy a.tar.gz to b.tar.gz (identical content).
	data, err := os.ReadFile(filepath.Join(dir, "a.tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.tar.gz"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	outputPath := filepath.Join(dir, "merged.tar.gz")
	c := &CLI{stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
	if err := c.runMergeSourceCmd(context.Background(), []string{
		"-o", outputPath,
		filepath.Join(dir, "a.tar.gz"),
		filepath.Join(dir, "b.tar.gz"),
	}); err != nil {
		t.Fatalf("runMergeSourceCmd() error = %v", err)
	}

	files := readTarGz(t, outputPath)
	if len(files) != 1 {
		t.Errorf("merged archive has %d files, want 1 (duplicate should be skipped)", len(files))
	}
}

func TestRunMergeSourceCmd_Conflict(t *testing.T) {
	dir := t.TempDir()

	// Create two archives with same path but different content.
	createTestTarGz(t, filepath.Join(dir, "a.tar.gz"), map[string]string{
		"/src/main.go": "package main // version 1\n",
	})
	createTestTarGz(t, filepath.Join(dir, "b.tar.gz"), map[string]string{
		"/src/main.go": "package main // version 2\n",
	})

	outputPath := filepath.Join(dir, "merged.tar.gz")
	c := &CLI{stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
	err := c.runMergeSourceCmd(context.Background(), []string{
		"-o", outputPath,
		filepath.Join(dir, "a.tar.gz"),
		filepath.Join(dir, "b.tar.gz"),
	})
	if err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Errorf("expected conflict error, got: %v", err)
	}
}

// writeJSON writes a CoverReport as JSON to a file in dir.
func writeJSON(t *testing.T, dir, name string, report tobari.CoverReport) {
	t.Helper()
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeJSONAt writes a CoverReport as JSON to the given absolute path,
// creating parent directories as needed.
func writeJSONAt(t *testing.T, path string, report tobari.CoverReport) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// minimalReport returns a minimal valid CoverReport for pattern-walk tests.
func minimalReport(testName, file string) tobari.CoverReport {
	return tobari.CoverReport{
		Metadata: tobari.CoverReportMetadata{
			Files: []string{file},
			Entry: []string{"FileName", "StartLine", "StartCol", "EndLine", "EndCol", "StatementCount"},
			All:   [][]int{{0, 1, 1, 2, 1, 1}},
		},
		Counts: []*tobari.CoverReportCount{
			{Name: testName, Coverprofile: [][]int{{0, 1}}},
		},
		AllCounts: []int{1},
	}
}

// createTestTarGz creates a tar.gz archive with the given path->content map.
func createTestTarGz(t *testing.T, path string, files map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	gw := gzip.NewWriter(f)
	tw := tar.NewWriter(gw)

	// Sort keys for deterministic output.
	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[i] > keys[j] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}

	for _, name := range keys {
		content := []byte(files[name])
		if err := tw.WriteHeader(&tar.Header{
			Name: name,
			Mode: 0o600,
			Size: int64(len(content)),
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
}

// readTarGz reads a tar.gz file and returns path->content map.
func readTarGz(t *testing.T, path string) map[string]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	gr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = gr.Close() }()

	result := make(map[string]string)
	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err != nil {
			break
		}
		var buf bytes.Buffer
		if _, err := buf.ReadFrom(tr); err != nil {
			t.Fatal(err)
		}
		result[hdr.Name] = buf.String()
	}
	return result
}
