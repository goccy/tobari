package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestIsToolexecCall(t *testing.T) {
	tests := []struct {
		name     string
		arg      string
		expected bool
	}{
		{"absolute path (compile)", "/usr/local/go/pkg/tool/darwin_arm64/compile", true},
		{"absolute path (link)", "/usr/local/go/pkg/tool/darwin_arm64/link", true},
		{"absolute path (vet)", "/opt/go/pkg/tool/linux_amd64/vet", true},
		{"subcommand flags", "flags", false},
		{"subcommand version", "version", false},
		{"subcommand help", "help", false},
		{"flag -v", "-v", false},
		{"flag --version", "--version", false},
		{"flag -h", "-h", false},
		{"flag --help", "--help", false},
		{"relative path", "./some/path", false},
		{"relative path with dots", "../bin/tool", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isToolexecCall(tt.arg)
			if got != tt.expected {
				t.Errorf("isToolexecCall(%q) = %v, want %v", tt.arg, got, tt.expected)
			}
		})
	}
}

func TestCLI_ShowVersion(t *testing.T) {
	var stdout bytes.Buffer
	c := &CLI{stdout: &stdout, stderr: &bytes.Buffer{}}

	err := c.showVersion()
	if err != nil {
		t.Fatalf("showVersion() error = %v", err)
	}

	output := stdout.String()
	if !strings.Contains(output, "tobari version") {
		t.Errorf("showVersion() output = %q, want to contain 'tobari version'", output)
	}
}

func TestCLI_ShowHelp(t *testing.T) {
	var stdout bytes.Buffer
	c := &CLI{stdout: &stdout, stderr: &bytes.Buffer{}}

	err := c.showHelp()
	if err != nil {
		t.Fatalf("showHelp() error = %v", err)
	}

	output := stdout.String()
	if !strings.Contains(output, "Usage:") {
		t.Errorf("showHelp() output = %q, want to contain 'Usage:'", output)
	}
	if !strings.Contains(output, "Commands:") {
		t.Errorf("showHelp() output = %q, want to contain 'Commands:'", output)
	}
}

func TestCLI_Run(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantContain string
		wantErr     bool
	}{
		{
			name:        "no args shows help",
			args:        []string{"tobari"},
			wantContain: "Usage:",
		},
		{
			name:        "version subcommand",
			args:        []string{"tobari", "version"},
			wantContain: "tobari version",
		},
		{
			name:        "help subcommand",
			args:        []string{"tobari", "help"},
			wantContain: "Usage:",
		},
		{
			name:        "-v flag",
			args:        []string{"tobari", "-v"},
			wantContain: "tobari version",
		},
		{
			name:        "--version flag",
			args:        []string{"tobari", "--version"},
			wantContain: "tobari version",
		},
		{
			name:        "-h flag",
			args:        []string{"tobari", "-h"},
			wantContain: "Usage:",
		},
		{
			name:        "--help flag",
			args:        []string{"tobari", "--help"},
			wantContain: "Usage:",
		},
		{
			name:    "unknown command",
			args:    []string{"tobari", "unknown"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			c := &CLI{stdout: &stdout, stderr: &stderr}
			ctx := context.Background()

			err := c.Run(ctx, tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("Run() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && !strings.Contains(stdout.String(), tt.wantContain) {
				t.Errorf("Run() output = %q, want to contain %q", stdout.String(), tt.wantContain)
			}
		})
	}
}
