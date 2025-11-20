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
		if _, err := fmt.Fprint(os.Stdout, string(out)); err != nil {
			return err
		}
		return nil
	}

	toolPath := args[1]
	toolName := filepath.Base(toolPath)
	toolArgs, err := filterCoveragecfg(args[2:])
	if err != nil {
		return err
	}

	if toolName != "cover" || isCoverVOption(toolArgs) || containsSelfPackageFile(toolArgs) {
		runCommand(toolPath, toolArgs)
		if err := saveArchiveFilePathIfRuntimePkgCompilation(toolName, toolArgs); err != nil {
			return err
		}
		return nil
	}

	return cover.Run(ctx, toolArgs)
}

func filterCoveragecfg(args []string) ([]string, error) {
	ret := make([]string, 0, len(args))
	for _, arg := range args {
		if strings.HasPrefix(arg, "-coveragecfg=") {
			coveragecfgPath := strings.TrimPrefix(arg, "-coveragecfg=")
			if err := addRuntimePkgIfNeeded(detectImportcfgPath(coveragecfgPath)); err != nil {
				return nil, err
			}
			continue
		}
		ret = append(ret, arg)
	}
	return ret, nil
}

func detectImportcfgPath(coveragecfgPath string) string {
	return filepath.Join(filepath.Dir(coveragecfgPath), "importcfg")
}

func addRuntimePkgIfNeeded(importcfgPath string) error {
	importcfg, err := os.ReadFile(importcfgPath)
	if err != nil {
		return fmt.Errorf("failed to read importcfg path %s: %w", importcfgPath, err)
	}
	if strings.Contains(string(importcfg), "packagefile runtime=") {
		// importcfg already has runtime package definition.
		return nil
	}
	archiveFilePath, err := os.ReadFile(runtimePkgArchiveFilePath())
	if err != nil {
		return fmt.Errorf("failed to read runtime package archive file path: %w", err)
	}
	runtimePkgDef := fmt.Sprintf("packagefile runtime=%s", string(archiveFilePath))
	content := string(importcfg) + "\n" + runtimePkgDef
	if err := os.WriteFile(importcfgPath, []byte(content), 0o600); err != nil {
		return fmt.Errorf("failed to overwrite importcfg: %w", err)
	}
	return nil
}

func tobariRoot() string {
	return filepath.Join(os.TempDir(), "tobari")
}

func runtimePkgArchiveFilePath() string {
	return filepath.Join(tobariRoot(), "runtime_pkg_archive_path.txt")
}

func isCoverVOption(args []string) bool {
	return len(args) == 1 && strings.HasPrefix(args[0], "-V")
}

func saveArchiveFilePathIfRuntimePkgCompilation(toolName string, toolArgs []string) error {
	if toolName != "compile" {
		return nil
	}
	var (
		archiveFilePath string
		isRuntimePkg    bool
	)
	for i := 0; i < len(toolArgs); i++ {
		opt := toolArgs[i]
		switch opt {
		case "-p":
			if i+1 < len(toolArgs) && toolArgs[i+1] == "runtime" {
				isRuntimePkg = true
			}
		case "-o":
			if i+1 < len(toolArgs) {
				archiveFilePath = toolArgs[i+1]
			}
		}
	}
	if !isRuntimePkg || archiveFilePath == "" {
		return nil
	}

	if err := os.MkdirAll(tobariRoot(), 0o755); err != nil {
		return fmt.Errorf("failed to create tobari root directory: %w", err)
	}
	if err := os.WriteFile(runtimePkgArchiveFilePath(), []byte(archiveFilePath), 0o600); err != nil {
		return fmt.Errorf("failed to create runtime package archive file path: %w", err)
	}
	return nil
}

func containsSelfPackageFile(args []string) bool {
	for _, arg := range args {
		if strings.Contains(arg, "/tobari/") && strings.HasSuffix(arg, ".go") {
			if strings.Contains(arg, "/tobari/internal") {
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
