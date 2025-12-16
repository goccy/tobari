package tool

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

var tobariCmd string

func Handle(ctx context.Context, args []string) error {
	tobariCmd = args[0]
	toolPath := args[1]
	toolArgs := args[2:]

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
