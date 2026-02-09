package utils

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
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

func GoPkgFiles(pkgPath string) []string {
	var ret []string
	_ = filepath.Walk(pkgPath, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || filepath.Ext(info.Name()) != ".go" {
			return nil
		}

		if strings.HasSuffix(info.Name(), "_test.go") {
			return nil
		}

		ret = append(ret, path)
		return nil
	})
	return ret
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

// GoListExportMap runs `go list -export -json` for the given packages
// and returns a map of import path -> export file path.
func GoListExportMap(pkgs []string) (map[string]string, error) {
	bin, err := GoBin()
	if err != nil {
		return nil, err
	}
	args := append([]string{"list", "-export", "-json"}, pkgs...)
	out, err := exec.Command(bin, args...).Output()
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
