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

// GoListExportMap runs `go list -export -json` for the given packages
// and returns a map of import path -> export file path.
// When toolexec and trimpath are provided, they are passed to `go list`
// so that the compiled packages use the same build cache entries as the
// outer build, ensuring matching fingerprints at link time.
func GoListExportMap(pkgs []string, toolexec string, trimpath bool, race bool) (map[string]string, error) {
	bin, err := GoBin()
	if err != nil {
		return nil, err
	}
	args := []string{"list", "-export", "-json"}
	if toolexec != "" {
		args = append(args, "-toolexec="+toolexec)
	}
	if trimpath {
		args = append(args, "-trimpath")
	}
	if race {
		args = append(args, "-race")
	}
	args = append(args, pkgs...)
	cmd := exec.Command(bin, args...)
	// Strip GOFLAGS to prevent tobari's -toolexec from being inherited,
	// which would cause recursive invocations.
	cmd.Env = filterGOFLAGSEnvs()
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to run go list: %w", err)
	}
	return parseGoListExportJSON(out)
}

// GoListDepsExport runs `go list -deps -export -json` in the specified directory.
// It strips GOFLAGS and explicitly passes -toolexec to avoid recursive invocations
// and to ensure the inner build uses exactly the specified toolexec command.
//
// When trimpath is true, -trimpath is passed to go list so that Go computes
// the same -trimpath value and action ID as the outer build. This ensures that
// the inner build produces standard library packages in the same build cache
// entries as the outer build, preventing fingerprint mismatches at link time.
func GoListDepsExport(dir string, toolexec string, trimpath bool, race bool, pkg string) (map[string]string, error) {
	bin, err := GoBin()
	if err != nil {
		return nil, err
	}
	args := []string{"list", "-deps", "-export", "-json", "-toolexec=" + toolexec}
	if trimpath {
		args = append(args, "-trimpath")
	}
	if race {
		args = append(args, "-race")
	}
	args = append(args, pkg)
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	cmd.Env = filterGOFLAGSEnvs()
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to run go list -deps: %w", err)
	}
	return parseGoListExportJSON(out)
}

func parseGoListExportJSON(data []byte) (map[string]string, error) {
	ret := make(map[string]string)
	decoder := json.NewDecoder(bytes.NewReader(data))
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

func AppPath() string {
	return filepath.Join(TobariTempDir(), "app", strconv.Itoa(os.Getppid()))
}