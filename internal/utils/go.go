package utils

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const tobariBuildIDEnv = "TOBARI_BUILD_ID"

// BuildID returns a unique ID for each "go build" execution.
// When TOBARI_BUILD_ID is set (by recursive buildPackages calls),
// it is used to ensure overlay path consistency across parent and child builds.
// Otherwise, os.Getppid() is used since toolexec processes are children of "go build".
func BuildID() string {
	if id := os.Getenv(tobariBuildIDEnv); id != "" {
		return id
	}
	return strconv.Itoa(os.Getppid())
}

func GoRoot() (string, error) {
	// check GOROOT environment variable first (set by toolchain switching)
	if root := os.Getenv("GOROOT"); root != "" {
		return root, nil
	}
	cmd, err := exec.LookPath("go")
	if err != nil {
		return "", fmt.Errorf("failed to find go binary path: %w", err)
	}
	out, err := exec.Command(cmd, "env", "GOROOT").Output()
	if err != nil {
		return "", fmt.Errorf("failed to get GOROOT: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func GoPkgPath(pkg string) (string, error) {
	root, err := GoRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "src", pkg), nil
}

func GoPkgFiles(pkgPath string) ([]string, error) {
	entries, err := os.ReadDir(pkgPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory %s: %w", pkgPath, err)
	}
	var ret []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") {
			continue
		}
		ret = append(ret, filepath.Join(pkgPath, name))
	}
	return ret, nil
}

func GoBin() (string, error) {
	root, err := GoRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "bin", "go"), nil
}

func GoVersion() (string, error) {
	bin, err := GoBin()
	if err != nil {
		return "", err
	}
	out, err := exec.Command(bin, "env", "GOVERSION").Output()
	if err != nil {
		return "", fmt.Errorf("failed to get GOVERSION from %s: %w", bin, err)
	}
	return strings.TrimSpace(string(out)), nil
}

func GoModTidy(dir string) error {
	bin, err := GoBin()
	if err != nil {
		return err
	}
	goRoot, err := GoRoot()
	if err != nil {
		return err
	}
	cmd := exec.Command(bin, "mod", "tidy")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOROOT="+goRoot)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to run 'go mod tidy' for app %s: %s: %w", dir, string(out), err)
	}
	return nil
}

func GoBuild(path string, args ...string) error {
	goBin, err := GoBin()
	if err != nil {
		return fmt.Errorf("failed to get go binary path: %w", err)
	}
	cmd := exec.Command(goBin, append([]string{"build"}, args...)...)
	cmd.Dir = path
	cmd.Env = append(
		filterGOFLAGSEnvs(),
		tobariBuildIDEnv+"="+BuildID(),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to build %s: %w", string(out), err)
	}
	return nil
}

// GoListExportMap runs `go list -export -json` for the given packages
// and returns a map of import path -> export file path.
func GoListExportMap(pkgs []string) (map[string]string, error) {
	bin, err := GoBin()
	if err != nil {
		return nil, err
	}
	args := append([]string{"list", "-export", "-json"}, pkgs...)
	cmd := exec.Command(bin, args...)
	// Without this, go list would invoke tobari recursively with a different
	// BuildID, causing overlay files to be at different paths, which leads to
	// fingerprint mismatches at link time.
	cmd.Env = filterGOFLAGSEnvs()
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to run go list: %w", err)
	}

	ret := make(map[string]string)
	decoder := json.NewDecoder(bytes.NewReader(out))
	for decoder.More() {
		var pkg struct {
			ImportPath string
			Export     string
		}
		if err := decoder.Decode(&pkg); err != nil {
			return nil, fmt.Errorf("failed to decode go list output: %w", err)
		}
		if pkg.Export != "" {
			ret[pkg.ImportPath] = pkg.Export
		}
	}
	return ret, nil
}

// envs stripped GOFLAGS to prevent -cover/-toolexec.
func filterGOFLAGSEnvs() []string {
	envs := os.Environ()
	newEnvs := make([]string, 0, len(envs))
	for _, kv := range envs {
		i := strings.IndexByte(kv, '=')
		if kv[:i] == "GOFLAGS" {
			continue
		}
		newEnvs = append(newEnvs, kv)
	}
	return newEnvs
}

// ImportsFromSource extracts import packages from Go source.
func ImportsFromSource(src []byte) ([]string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", src, parser.ImportsOnly)
	if err != nil {
		return nil, err
	}

	imports := make([]string, 0, len(file.Imports))
	for _, imp := range file.Imports {
		v, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			return nil, err
		}
		imports = append(imports, v)
	}
	return imports, nil
}

func TobariTempDir() string {
	return filepath.Join(os.TempDir(), "tobari")
}

func OverlayDir() string {
	return filepath.Join(TobariTempDir(), "overlay")
}

func TobariPkgJSONPath() string {
	return filepath.Join(TobariTempDir(), "builds", BuildID(), "tobari_pkg.json")
}

func AppPath() string {
	return filepath.Join(TobariTempDir(), "builds", BuildID(), "app", strconv.Itoa(os.Getpid()))
}
