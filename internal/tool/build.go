package tool

import (
	"fmt"
	"os"
	"strings"

	"github.com/goccy/tobari/internal/version"
)

func getTobariPkgs(args []string) (map[string]string, error) {
	ver, err := version.Get()
	if err != nil {
		return nil, err
	}
	pkgs, err := buildPackages(ver, getLangFromArgs(args))
	if err != nil {
		return nil, fmt.Errorf("failed to build temp module: %w", err)
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

	var newEntries strings.Builder
	for importPath, pkgPath := range pkgs {
		// Skip packages that already exist in the importcfg
		if strings.Contains(content, "packagefile "+importPath+"=") {
			continue
		}
		fmt.Fprintf(&newEntries, "packagefile %s=%s\n", importPath, pkgPath)
	}

	if newEntries.Len() == 0 {
		return nil
	}

	content = newEntries.String() + content
	if err := os.WriteFile(importCfgPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("failed to write updated importcfg: %w", err)
	}
	return nil
}
