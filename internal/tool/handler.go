package tool

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
		if err := handleCover(ctx, toolPath, toolArgs, opts.EmbedCode, opts.ExcludeAnalysis); err != nil {
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
	ident := fmt.Sprintf("version:%s overlay:%s opt:%s", ver.ID(), overlayHash, opts.Hash())

	// -exclude-analysis only changes the suppDeps produced by the cover tool, so
	// it is folded into the *cover* tool's identity alone. Go probes -V=full per
	// tool, and each tool's identity feeds the actionID of the packages built
	// with it. Adding this to the compile tool's identity instead would
	// invalidate the entire dependency closure, even though only the
	// cover-instrumented packages and main can change.
	//
	// It cannot be handled at compile time instead: on a cache hit Go skips the
	// toolexec invocation entirely, so nothing written during compile can affect
	// the hit/miss decision. The identity probe is the only pre-action input.
	if filepath.Base(toolPath) == "cover" && len(opts.ExcludeAnalysis) != 0 {
		ident += " exclude-analysis:" + hashStrings(opts.ExcludeAnalysis)
	}

	fmt.Printf("%s tobari:[%s]\n", strings.TrimSpace(string(org)), ident)
	return nil
}

// hashStrings returns a short stable hash of a string slice.
func hashStrings(v []string) string {
	h := sha256.Sum256([]byte(strings.Join(v, "\x00")))
	return hex.EncodeToString(h[:])[:16]
}
