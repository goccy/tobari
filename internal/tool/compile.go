package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/goccy/tobari/internal/overlay"
	"github.com/goccy/tobari/internal/utils"
)

func handleCompile(ctx context.Context, toolPath string, args []string) error {
	pkgName := getPkgNameFromArgs(args)

	// Check if this package needs overlay
	if def, ok := overlay.TargetPackages()[pkgName]; ok {
		// Collect source files from args
		var sourceFiles []string
		for _, arg := range args {
			if filepath.Ext(arg) == ".go" {
				sourceFiles = append(sourceFiles, arg)
			}
		}

		// Render overlay for this package only
		pkg, err := overlay.RenderPackage(def, sourceFiles)
		if err != nil {
			return fmt.Errorf("failed to render overlay for %s: %w", pkgName, err)
		}

		// Replace modified file paths in args
		for i, arg := range args {
			if newPath, ok := pkg.Replace[arg]; ok {
				args[i] = newPath
			}
		}
		// Add new tobari.go file
		args = append(args, pkg.Added...)

		// Add missing imports to importcfg
		if err := addMissingImportsToImportcfg(args, pkg.Imports); err != nil {
			return err
		}
	}

	args, err := filterCoveragecfg(args)
	if err != nil {
		return err
	}
	if err := addTobariPkgsToImportcfgFromCompileOptions(args); err != nil {
		return err
	}
	runCommand(toolPath, args)
	return nil
}

// addTobariPkgsToImportcfgFromCompileOptions
// dynamically add an import statement for github.com/goccy/tobari in testmain.go,
// there may be cases where it doesn't exist in the importcfg as is.
// In such cases, if the target test uses github.com/goccy/tobari, linking is possible;
// however, if it doesn't use it, a tobari package must be created dynamically and its path specified.
func addTobariPkgsToImportcfgFromCompileOptions(args []string) error {
	importCfgPath := getImportcfgPathFromArgs(args)

	var goFiles []string
	for _, arg := range args {
		if filepath.Ext(arg) != ".go" {
			continue
		}
		goFiles = append(goFiles, arg)
	}
	if importCfgPath == "" || len(goFiles) == 0 {
		return nil
	}

	data, err := os.ReadFile(importCfgPath)
	if err != nil {
		return fmt.Errorf("failed to read importcfg for testmain: %w", err)
	}

	if strings.Contains(string(data), "github.com/goccy/tobari") {
		return nil
	}

	var usedTobariPkg bool
	for _, goFile := range goFiles {
		src, err := os.ReadFile(goFile)
		if err != nil {
			return fmt.Errorf("failed to read file: %w", err)
		}
		imports, err := utils.ImportsFromSource(src)
		if err != nil {
			return fmt.Errorf("failed to parse file %s: %w", goFile, err)
		}
		for _, imp := range imports {
			if strings.Contains(imp, "github.com/goccy/tobari") {
				usedTobariPkg = true
				goto SEARCH_TOBARI_PKG_END
			}
		}
	}
SEARCH_TOBARI_PKG_END:

	if !usedTobariPkg {
		return nil
	}

	pkgs, err := getTobariPkgs(args)
	if err != nil {
		return err
	}
	if err := overwriteImportcfg(importCfgPath, pkgs); err != nil {
		return fmt.Errorf("failed to update importcfg: %w", err)
	}
	return nil
}

func getPkgNameFromArgs(args []string) string {
	for i := 0; i < len(args); i++ {
		if args[i] == "-p" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// addMissingImportsToImportcfg adds missing import entries to the importcfg
// for packages imported by the overlay's tobari.go file.
func addMissingImportsToImportcfg(args []string, imports []string) error {
	importCfgPath := getImportcfgPathFromArgs(args)
	if importCfgPath == "" {
		return nil
	}

	// Read current importcfg
	data, err := os.ReadFile(importCfgPath)
	if err != nil {
		return fmt.Errorf("failed to read importcfg %s: %w", importCfgPath, err)
	}
	importCfgContent := string(data)

	// Find imports that are missing from importcfg
	var missingImports []string
	for _, importPath := range imports {
		if importPath == "unsafe" {
			continue
		}
		if strings.Contains(importCfgContent, importPath+"=") {
			continue
		}
		missingImports = append(missingImports, importPath)
	}

	if len(missingImports) == 0 {
		return nil
	}

	// Get export paths for missing imports via go list
	exportPaths, err := utils.GoListExportMap(missingImports)
	if err != nil {
		return fmt.Errorf("failed to get export paths: %w", err)
	}

	// Build new importcfg entries
	var newEntries strings.Builder
	for importPath, exportPath := range exportPaths {
		newEntries.WriteString(fmt.Sprintf("packagefile %s=%s\n", importPath, exportPath))
	}

	if newEntries.Len() == 0 {
		return nil
	}

	// Prepend new entries to importcfg
	newContent := newEntries.String() + importCfgContent
	if err := os.WriteFile(importCfgPath, []byte(newContent), 0o644); err != nil {
		return fmt.Errorf("failed to write updated importcfg: %w", err)
	}
	return nil
}

func filterCoveragecfg(args []string) ([]string, error) {
	ret := make([]string, 0, len(args))
	for _, arg := range args {
		if strings.HasPrefix(arg, "-coveragecfg=") {
			continue
		}
		ret = append(ret, arg)
	}
	return ret, nil
}


