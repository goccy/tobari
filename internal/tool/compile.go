package tool

import (
	"context"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"github.com/goccy/tobari/internal/overlay"
)

func handleCompile(ctx context.Context, toolPath string, args []string) error {
	// Replace file paths and add new files based on overlay Replace map.
	// This handles the case where Go's overlay mechanism doesn't work
	// (e.g., when toolchain is installed in GOMODCACHE).
	replace, err := overlay.GetReplace(ctx)
	if err != nil {
		return fmt.Errorf("failed to get overlay replace map: %w", err)
	}
	args, addedFiles, err := applyOverlayReplacements(args, replace)
	if err != nil {
		return fmt.Errorf("failed to apply overlay replacements: %w", err)
	}

	// If new tobari.go files were added, ensure their imports are in the importcfg
	if len(addedFiles) > 0 {
		if err := addMissingImportsToImportcfg(ctx, args, addedFiles); err != nil {
			return err
		}
	}

	args, err = filterCoveragecfg(args)
	if err != nil {
		return err
	}
	if err := addTobariPkgsToImportcfgFromCompileOptions(toolPath, args); err != nil {
		return err
	}
	runCommand(toolPath, args)
	if err := saveRuntimePkgIfPresent(args); err != nil {
		return err
	}
	return nil
}

// addTobariPkgsToImportcfgFromCompileOptions
// dynamically add an import statement for github.com/goccy/tobari in testmain.go,
// there may be cases where it doesn't exist in the importcfg as is.
// In such cases, if the target test uses github.com/goccy/tobari, linking is possible;
// however, if it doesn't use it, a tobari package must be created dynamically and its path specified.
func addTobariPkgsToImportcfgFromCompileOptions(toolPath string, args []string) error {
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
		fset := new(token.FileSet)
		f, err := os.ReadFile(goFile)
		if err != nil {
			return fmt.Errorf("failed to read file: %w", err)
		}
		file, err := parser.ParseFile(fset, "", string(f), parser.ImportsOnly)
		if err != nil {
			return fmt.Errorf("failed to parse file %s: %w", goFile, err)
		}
		for _, imprt := range file.Imports {
			if strings.Contains(imprt.Path.Value, "github.com/goccy/tobari") {
				usedTobariPkg = true
				goto SEARCH_TOBARI_PKG_END
			}
		}
	}
SEARCH_TOBARI_PKG_END:

	if !usedTobariPkg {
		return nil
	}

	goRoot := goRootFromToolPath(toolPath)
	pkgs, err := getTobariPkgs(args, goRoot)
	if err != nil {
		return err
	}
	if err := overwriteImportcfg(importCfgPath, pkgs); err != nil {
		return fmt.Errorf("failed to update importcfg: %w", err)
	}
	return nil
}

// applyOverlayReplacements replaces file paths and adds new files based on overlay Replace map.
// This is essential for toolchain directive support where Go is installed in GOMODCACHE
// and the standard overlay mechanism may not work correctly.
// Returns the modified args and a list of new files that were added.
func applyOverlayReplacements(args []string, replace map[string]string) ([]string, []string, error) {
	// Build a set of existing file paths in args for quick lookup
	existingFiles := make(map[string]bool)
	for _, arg := range args {
		if filepath.Ext(arg) == ".go" {
			existingFiles[arg] = true
			// Also add the absolute path
			abs, err := filepath.Abs(arg)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to get absolute path for %s: %w", arg, err)
			}
			existingFiles[abs] = true
		}
	}

	// Get the package name from -p flag
	pkgName := getPkgNameFromArgs(args)

	// Collect new files to add
	var newFiles []string
	for origPath, newPath := range replace {
		// Check if this is a new file (tobari.go) for the current package
		if strings.HasSuffix(origPath, "/tobari.go") {
			// Extract package name from origPath
			// e.g., /usr/local/go/src/runtime/tobari.go -> runtime
			// e.g., /usr/local/go/src/testing/internal/testdeps/tobari.go -> testing/internal/testdeps
			if idx := strings.Index(origPath, "/src/"); idx != -1 {
				pkgFromPath := filepath.Dir(origPath[idx+5:]) // skip "/src/"
				// Check if this tobari.go belongs to the current package
				if pkgFromPath == pkgName {
					// Only add if not already in args
					if !existingFiles[newPath] {
						newFiles = append(newFiles, newPath)
					}
				}
			}
		}
	}

	// Replace existing file paths in args
	for i, arg := range args {
		if filepath.Ext(arg) != ".go" {
			continue
		}
		// Try to match the arg with an original path in replace map
		absArg, err := filepath.Abs(arg)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get absolute path for %s: %w", arg, err)
		}
		if newPath, ok := replace[absArg]; ok {
			// Replace with the overlay path if not already replaced
			if args[i] != newPath {
				args[i] = newPath
			}
		}
	}

	// Add new files to the compile arguments
	return append(args, newFiles...), newFiles, nil
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
// for packages imported by newly added tobari.go files.
func addMissingImportsToImportcfg(ctx context.Context, args []string, addedFiles []string) error {
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

	// Collect all imports from added files
	neededImports := make(map[string]bool)
	for _, filePath := range addedFiles {
		content, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("failed to read added file %s: %w", filePath, err)
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, "", content, parser.ImportsOnly)
		if err != nil {
			return fmt.Errorf("failed to parse added file %s: %w", filePath, err)
		}
		for _, imp := range file.Imports {
			importPath := strings.Trim(imp.Path.Value, "\"")
			// Skip if already in importcfg
			if strings.Contains(importCfgContent, importPath+"=") {
				continue
			}
			// Skip unsafe as it doesn't have a package file
			if importPath == "unsafe" {
				continue
			}
			neededImports[importPath] = true
		}
	}

	if len(neededImports) == 0 {
		return nil
	}

	// Get export paths from overlay
	exportPaths, err := overlay.GetExportPaths(ctx)
	if err != nil {
		return fmt.Errorf("failed to get export paths from overlay: %w", err)
	}
	if exportPaths == nil {
		return nil
	}

	// Build new importcfg entries
	var newEntries strings.Builder
	for importPath := range neededImports {
		if exportPath, ok := exportPaths[importPath]; ok {
			newEntries.WriteString(fmt.Sprintf("packagefile %s=%s\n", importPath, exportPath))
		}
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

// The path from when the runtime package was built is saved and later used when adding it to the importcfg.
func saveRuntimePkgIfPresent(args []string) error {
	var (
		pkgPath      string
		isRuntimePkg bool
	)
	for i := 0; i < len(args); i++ {
		opt := args[i]
		switch opt {
		case "-p":
			if i+1 < len(args) && args[i+1] == "runtime" {
				isRuntimePkg = true
			}
		case "-o":
			if i+1 < len(args) {
				pkgPath = args[i+1]
			}
		}
	}
	if !isRuntimePkg || pkgPath == "" {
		return nil
	}

	if err := writeRuntimePkg([]byte(pkgPath)); err != nil {
		return err
	}
	return nil
}
