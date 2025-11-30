package main

import (
	"context"
	"encoding/json"
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
	if toolName == "vet" {
		if err := replaceVetCfgIfNeeded(toolArgs); err != nil {
			return err
		}
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
			if err := writeCoveragecfgDir(filepath.Dir(coveragecfgPath)); err != nil {
				return nil, err
			}
			continue
		}
		ret = append(ret, arg)
	}
	return ret, nil
}

func replaceVetCfgIfNeeded(args []string) error {
	for _, arg := range args {
		if strings.HasSuffix(arg, "vet.cfg") {
			coveragecfgDir, err := readCoveragecfgDir()
			if err != nil {
				// ignore if can't find vet.cfg path.
				return nil
			}
			vetcfgPath := filepath.Join(coveragecfgDir, "vet.cfg")
			vetcfg, err := os.ReadFile(vetcfgPath)
			if err != nil {
				return err
			}
			var m map[string]any
			if err := json.Unmarshal(vetcfg, &m); err != nil {
				return err
			}
			importMap, exists := m["ImportMap"]
			if !exists {
				return fmt.Errorf("failed to get ImportMap section from vet.cfg: %q", vetcfg)
			}
			typedImportMap, ok := importMap.(map[string]any)
			if !ok {
				return fmt.Errorf("failed to get ImportMap as map[string]string type: %q", vetcfg)
			}
			typedImportMap["runtime"] = "runtime"
			typedImportMap["unsafe"] = "unsafe"

			pkgFileMap, exists := m["PackageFile"]
			if !exists {
				return fmt.Errorf("failed to get PackageFile section from vet.cfg: %q", vetcfg)
			}
			typedPkgFileMap, ok := pkgFileMap.(map[string]any)
			if !ok {
				return fmt.Errorf("failed to get PackageFile as map[string]string type: %q", vetcfg)
			}
			if _, exists := typedPkgFileMap["runtime"]; !exists {
				archiveFilePath, err := readRuntimePkgArchiveFilePath()
				if err != nil {
					return err
				}
				typedPkgFileMap["runtime"] = string(archiveFilePath)
			}

			b, err := json.Marshal(m)
			if err != nil {
				return fmt.Errorf("failed to encode vet.cfg: %w", err)
			}
			if err := os.WriteFile(vetcfgPath, b, 0o600); err != nil {
				return fmt.Errorf("failed to rewrite vet.cfg: %w", err)
			}
		}
	}
	return nil
}

// Normally, the path in the importcfg file uses the "$WORK" variable.
// Since "$WORK" is not an environment variable and can only be resolved by the default Go tool, its actual path is usually unknown.
// However, when the -cover option is enabled, it is a rule that the path to the -coveragecfg option is passed at compile time, and that path is already resolved.
// Furthermore, there is also a rule that both the coveragecfg and importcfg files are written to the same directory.
// Therefore, it is possible to deduce the path to the importcfg file from the path to the coveragecfg file.
func detectImportcfgPath(coveragecfgPath string) string {
	return filepath.Join(filepath.Dir(coveragecfgPath), "importcfg")
}

// In Tobari, the AST of the files targeted for coverage measurement is manipulated,
// and the GID() and PGID() functions—dynamically added to the runtime package using the overlay option—are used to measure the Goroutine ID.
// However, if the original file being measured does not import the runtime package, the importcfg file will not contain an archive file path entry for the runtime package,
// resulting in a compilation failure due to unresolved references to the runtime package.
// To prevent this, a reference to the runtime package is dynamically added to the importcfg file.
// Since the runtime package is always built in advance, its archive file path is stored and used to resolve the reference.
func addRuntimePkgIfNeeded(importcfgPath string) error {
	importcfg, err := os.ReadFile(importcfgPath)
	if err != nil {
		return fmt.Errorf("failed to read importcfg path %s: %w", importcfgPath, err)
	}
	if strings.Contains(string(importcfg), "packagefile runtime=") {
		// importcfg already has runtime package definition.
		return nil
	}
	archiveFilePath, err := readRuntimePkgArchiveFilePath()
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

func isCoverVOption(args []string) bool {
	return len(args) == 1 && strings.HasPrefix(args[0], "-V")
}

// The path from when the runtime package was built is saved and later used when adding it to the importcfg.
// Currently, a single temporary path is used for storing this value, which can lead to race conditions if multiple build processes run concurrently.
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

	if err := writeRuntimePkgArchiveFilePath([]byte(archiveFilePath)); err != nil {
		return err
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

func readRuntimePkgArchiveFilePath() (string, error) {
	f, err := os.ReadFile(runtimePkgArchiveFilePath())
	if err != nil {
		return "", fmt.Errorf("failed to read runtime package archive file path from %s: %w", runtimePkgArchiveFilePath(), err)
	}
	return strings.TrimSpace(string(f)), nil
}

func writeRuntimePkgArchiveFilePath(b []byte) error {
	if err := os.MkdirAll(tobariRoot(), 0o755); err != nil {
		return fmt.Errorf("failed to create tobari root directory: %w", err)
	}
	if err := os.WriteFile(runtimePkgArchiveFilePath(), b, 0o600); err != nil {
		return fmt.Errorf("failed to create runtime package archive file path %s: %w", runtimePkgArchiveFilePath(), err)
	}
	return nil
}

func runtimePkgArchiveFilePath() string {
	return filepath.Join(tobariRoot(), fmt.Sprintf("runtime_pkg_archive_path_%s.txt", buildID()))
}

func readCoveragecfgDir() (string, error) {
	f, err := os.ReadFile(coveragecfgDir())
	if err != nil {
		return "", fmt.Errorf("failed to read coveragecfg directory from %s: %w", coveragecfgDir(), err)
	}
	return strings.TrimSpace(string(f)), nil
}

func writeCoveragecfgDir(dir string) error {
	if err := os.MkdirAll(tobariRoot(), 0o755); err != nil {
		return fmt.Errorf("failed to create tobari root directory: %w", err)
	}
	if err := os.WriteFile(coveragecfgDir(), []byte(dir), 0o600); err != nil {
		return fmt.Errorf("failed to create %s: %w", coveragecfgDir(), err)
	}
	return nil
}

func coveragecfgDir() string {
	return filepath.Join(tobariRoot(), fmt.Sprintf("coveragecfg_dir_%s.txt", buildID()))
}

func tobariRoot() string {
	return filepath.Join(os.TempDir(), "tobari")
}

// buildID returns a unique ID for each "go build" execution.
func buildID() string {
	// The "go tool" command is invoked by "go build",
	// so its parent process ID becomes the ID of the "go build" process.
	// In other words, it can be used as a unique ID for each "go build" execution.
	return fmt.Sprint(os.Getppid())
}
