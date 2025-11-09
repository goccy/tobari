package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/goccy/tobari/internal/cover"
	"github.com/goccy/tobari/internal/flags"
)

func main() {
	ctx := context.Background()
	if err := run(ctx, os.Args); err != nil {
		fmt.Fprint(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) < 2 {
		return nil
	}

	tobariBinPath := args[0]
	if len(args) == 2 && args[1] == "flags" {
		out, err := flags.Run(ctx, tobariBinPath)
		if err != nil {
			return err
		}
		fmt.Fprint(os.Stdout, string(out))
		return nil
	}

	toolPath := args[1]
	toolName := filepath.Base(toolPath)
	toolArgs := filterCoveragecfg(args[2:])

	if toolName != "cover" || isCoverVOption(toolArgs) || containsSelfPackageFile(toolArgs) {
		runCommand(toolPath, toolArgs)
		return nil
	}

	return cover.Run(ctx, toolArgs)
}

func filterCoveragecfg(args []string) []string {
	ret := make([]string, 0, len(args))
	for _, arg := range args {
		if strings.HasPrefix(arg, "-coveragecfg=") {
			continue
		}
		ret = append(ret, arg)
	}
	return ret
}

func isCoverVOption(args []string) bool {
	return len(args) == 1 && strings.HasPrefix(args[0], "-V")
}

func containsSelfPackageFile(args []string) bool {
	for _, arg := range args {
		if strings.Contains(arg, "/goccy/tobari/") && strings.HasSuffix(arg, ".go") {
			if strings.Contains(arg, "/goccy/tobari/internal") {
				return true
			}
			if filepath.Base(arg) == "tobari.go" {
				return true
			}
		}
	}
	return false
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
