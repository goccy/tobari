package tool

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/goccy/tobari/internal/overlay"
)

func Handle(ctx context.Context, args []string, embedCode bool) error {
	toolPath := args[1]
	toolArgs := args[2:]

	// Handle -V=full flag to isolate build cache
	if slices.Contains(toolArgs, "-V=full") {
		return handleVersionFull(ctx, toolPath, toolArgs)
	}

	toolName := filepath.Base(toolPath)
	switch toolName {
	case "compile":
		if err := handleCompile(ctx, toolPath, toolArgs); err != nil {
			return err
		}
	case "vet":
		if err := handleVet(ctx, toolPath, toolArgs); err != nil {
			return err
		}
	case "link":
		if err := handleLink(ctx, toolPath, toolArgs); err != nil {
			return err
		}
	case "cover":
		if err := handleCover(ctx, toolPath, toolArgs, embedCode); err != nil {
			return err
		}
	default:
		runCommand(toolPath, toolArgs)
	}
	return nil
}

func runCommand(bin string, args []string) {
	cmd := exec.Command(bin, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			os.Exit(ee.ExitCode())
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}

func handleVersionFull(ctx context.Context, toolPath string, args []string) error {
	org, err := exec.CommandContext(ctx, toolPath, args...).Output()
	if err != nil {
		return fmt.Errorf("failed to run -V=full: %w", err)
	}

	hash, err := overlay.ComputeHash()
	if err != nil {
		return fmt.Errorf("failed to compute overlay hash: %w", err)
	}

	// Output version with tobari hash suffix
	fmt.Printf("%s tobari:%s\n", strings.TrimSpace(string(org)), hash)
	return nil
}
