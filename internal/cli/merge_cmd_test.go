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

func TestRunMergeJSONCmd_TooFewInputs(t *testing.T) {
	c := &CLI{stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
	err := c.runMergeJSONCmd(context.Background(), []string{"only-one.json"})
	if err == nil || !strings.Contains(err.Error(), "at least two input files") {
		t.Errorf("expected input count error, got: %v", err)
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
