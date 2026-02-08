package tool

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/goccy/tobari/internal/version"
)

// goRootFromToolPath extracts GOROOT from a tool path.
// Tool paths have the structure: $GOROOT/pkg/tool/$GOOS_$GOARCH/compile (or link)
func goRootFromToolPath(toolPath string) string {
	return filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(toolPath))))
}

func getTobariPkgs(args []string, goRoot string) (map[string]string, error) {
	if pkgs := readTobariPkgs(buildID()); pkgs != nil {
		return pkgs, nil
	}

	pkgs, err := createTobariPkgs(getLangFromArgs(args), goRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to build temp module: %w", err)
	}
	return pkgs, nil
}

// createTobariPkgs automatically generate a minimal application for creating tobari pkgs,
// and save the resulting tobari pkgs and their paths when the application is built.
func createTobariPkgs(lang, goRoot string) (map[string]string, error) {
	if pkgs := readTobariPkgs(buildID()); pkgs != nil {
		return pkgs, nil
	}

	ver, err := version.Get()
	if err != nil {
		return nil, err
	}
	pkgs, err := buildPackages(ver, lang, goRoot)
	if err != nil {
		return nil, err
	}
	if err := writeTobariPkgs(pkgs); err != nil {
		return nil, err
	}
	return pkgs, nil
}

func getImportcfgPathFromArgs(args []string) string {
	for i, arg := range args {
		if arg == "-importcfg" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func getLangFromArgs(args []string) string {
	for _, arg := range args {
		if strings.HasPrefix(arg, "-lang=") {
			return strings.TrimPrefix(arg, "-lang=")
		}
	}
	return ""
}

func overwriteImportcfg(importCfgPath string, pkgs map[string]string) error {
	data, err := os.ReadFile(importCfgPath)
	if err != nil {
		return fmt.Errorf("failed to read importcfg: %w", err)
	}

	content := string(data)

	if strings.Contains(content, "github.com/goccy/tobari") {
		return nil
	}

	newContent := ""
	for importPath, pkgPath := range pkgs {
		tobariEntry := fmt.Sprintf("packagefile %s=%s\n", importPath, pkgPath)
		newContent += tobariEntry
	}

	content = newContent + content
	if err := os.WriteFile(importCfgPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write updated importcfg: %w", err)
	}
	return nil
}
