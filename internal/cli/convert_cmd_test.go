package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunConvertCmd_DefaultOutput(t *testing.T) {
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

	if err := c.runConvertCmd(context.Background(), []string{jsonPath}); err != nil {
		t.Fatalf("runConvertCmd() error = %v\nstderr: %s", err, stderr.String())
	}

	defaultOutput := filepath.Join(outDir, "cover.out")
	data, err := os.ReadFile(defaultOutput)
	if err != nil {
		t.Fatalf("default output file not created: %v", err)
	}
	if !strings.HasPrefix(string(data), "mode: set\n") {
		t.Errorf("output does not start with 'mode: set'")
	}
	if !strings.Contains(stdout.String(), "cover.out") {
		t.Errorf("stdout = %q, want to contain 'cover.out'", stdout.String())
	}
}

func TestRunConvertCmd_ToFile(t *testing.T) {
	srcDir := t.TempDir()
	srcPath := createTestGoFile(t, srcDir)
	jsonPath := createTobariJSON(t, srcDir, srcPath)
	outputPath := filepath.Join(t.TempDir(), "profile.cover")

	var stdout, stderr bytes.Buffer
	c := &CLI{stdout: &stdout, stderr: &stderr}

	if err := c.runConvertCmd(context.Background(), []string{"-o", outputPath, jsonPath}); err != nil {
		t.Fatalf("runConvertCmd() error = %v\nstderr: %s", err, stderr.String())
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("failed to read output file: %v", err)
	}
	if !strings.HasPrefix(string(data), "mode: set\n") {
		t.Errorf("output file does not start with 'mode: set'")
	}
	if !strings.Contains(stdout.String(), "Coverprofile written to") {
		t.Errorf("stdout = %q, want to contain success message", stdout.String())
	}
}

func TestRunConvertCmd_RejectsCoverprofile(t *testing.T) {
	srcDir := t.TempDir()
	srcPath := createTestGoFile(t, srcDir)
	profilePath := createCoverprofile(t, srcDir, srcPath)

	var stdout, stderr bytes.Buffer
	c := &CLI{stdout: &stdout, stderr: &stderr}

	if err := c.runConvertCmd(context.Background(), []string{profilePath}); err == nil {
		t.Fatal("expected error for coverprofile input, got nil")
	} else if !strings.Contains(err.Error(), "already in coverprofile format") {
		t.Errorf("error = %q, want to contain 'already in coverprofile format'", err.Error())
	}
}

func TestRunConvertCmd_Errors(t *testing.T) {
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
			name:       "flags after input file",
			args:       []string{"input.json", "-o", "out.cover"},
			wantErrMsg: "flags must be specified before the input file",
		},
		{
			name:       "nonexistent input file",
			args:       []string{"/nonexistent/path/input.json"},
			wantErrMsg: "failed to read input file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			c := &CLI{stdout: &stdout, stderr: &stderr}

			if err := c.runConvertCmd(context.Background(), tt.args); err == nil {
				t.Fatal("expected error, got nil")
			} else if !strings.Contains(err.Error(), tt.wantErrMsg) {
				t.Errorf("error = %q, want to contain %q", err.Error(), tt.wantErrMsg)
			}
		})
	}
}
