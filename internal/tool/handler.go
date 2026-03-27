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
	"github.com/goccy/tobari/internal/version"
)

func Handle(ctx context.Context, args []string, opts BuildOpts) error {
	toolPath := args[1]
	toolArgs := args[2:]

	// Handle -V=full flag to isolate build cache
	if slices.Contains(toolArgs, "-V=full") {
		return handleVersionFull(ctx, toolPath, toolArgs, opts)
	}

	toolName := filepath.Base(toolPath)
	switch toolName {
	case "compile":
		if err := handleCompile(ctx, toolPath, toolArgs, opts); err != nil {
			return err
		}
	case "vet":
		if err := handleVet(ctx, toolPath, toolArgs); err != nil {
			return err
		}
	case "link":
		if err := handleLink(ctx, toolPath, toolArgs, opts); err != nil {
			return err
		}
	case "cover":
		if err := handleCover(ctx, toolPath, toolArgs, opts.EmbedCode); err != nil {
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

func handleVersionFull(ctx context.Context, toolPath string, args []string, opts BuildOpts) error {
	org, err := exec.CommandContext(ctx, toolPath, args...).Output()
	if err != nil {
		return fmt.Errorf("failed to run -V=full: %w", err)
	}

	overlayHash, err := overlay.ComputeHash()
	if err != nil {
		return fmt.Errorf("failed to compute overlay hash: %w", err)
	}

	ver, err := version.Get()
	if err != nil {
		return fmt.Errorf("failed to get tobari version: %w", err)
	}

	// Output version with tobari identifiers; Go uses this as part of the build cache key.
	// - version: invalidates cache when tobari version changes (e.g., new release)
	// - overlay: invalidates cache when overlay files change (e.g., Go version update)
	// - opt: invalidates cache when toolexec options change (e.g., --embed-code)
	// Note: trimpath and race are excluded from opt because they are detected later
	// from compiler args and Go already includes them in its own cache key.
	fmt.Printf("%s tobari:[version:%s overlay:%s opt:%s]\n",
		strings.TrimSpace(string(org)), ver.ID(), overlayHash, opts.Hash())
	return nil
}
