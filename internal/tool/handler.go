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
	"sort"
	"strings"

	"github.com/goccy/tobari/internal/overlay"
)

func Handle(ctx context.Context, args []string) error {
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
		if err := handleCover(ctx, toolPath, toolArgs); err != nil {
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
	// Execute the original tool to get version info
	out, err := exec.CommandContext(ctx, toolPath, args...).Output()
	if err != nil {
		// If the tool fails, just run it normally
		runCommand(toolPath, args)
		return nil
	}

	// Get the Replace map from overlay.json
	replace, err := overlay.GetReplace(ctx)
	if err != nil {
		// If overlay.json doesn't exist, just output the original version
		fmt.Print(string(out))
		return nil
	}

	// Compute hash from the Replace map (sorted keys for deterministic output)
	keys := make([]string, 0, len(replace))
	for k := range replace {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	h := sha256.New()
	for _, k := range keys {
		h.Write([]byte(k))
		h.Write([]byte(replace[k]))
	}
	hash := hex.EncodeToString(h.Sum(nil))[:16]

	// Output version with tobari hash suffix
	fmt.Printf("%s tobari:%s\n", strings.TrimSpace(string(out)), hash)
	return nil
}
