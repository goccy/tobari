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
	if err == nil {
		args = applyOverlayReplacements(args, replace)
	}

	args, err = filterCoveragecfg(args)
	if err != nil {
		return err
	}
	if err := addTobariPkgsToImportcfgFromCompileOptions(args); err != nil {
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
		fset := new(token.FileSet)
		f, err := os.ReadFile(goFile)
		if err != nil {
			return fmt.Errorf("failed to read file: %w", err)
		}
		file, err := parser.ParseFile(fset, "", string(f), parser.ImportsOnly)
		if err != nil {
			continue
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

	pkgs, err := getTobariPkgs(args)
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
func applyOverlayReplacements(args []string, replace map[string]string) []string {
	// Build a set of existing file paths in args for quick lookup
	existingFiles := make(map[string]bool)
	for _, arg := range args {
		if filepath.Ext(arg) == ".go" {
			existingFiles[arg] = true
			// Also add the absolute path
			if abs, err := filepath.Abs(arg); err == nil {
				existingFiles[abs] = true
			}
		}
	}

	// Get the package name from -p flag
	pkgName := getPkgNameFromArgs(args)

	// Collect new files to add
	var newFiles []string
	for origPath, newPath := range replace {
		// Check if this is a new file (tobari.go) for the current package
		if strings.HasSuffix(origPath, "/tobari.go") {
			// Check if this tobari.go belongs to the current package
			// Use suffix matching to handle nested packages correctly
			// e.g., /usr/local/go/src/runtime/tobari.go matches package "runtime"
			//       /usr/local/go/src/testing/internal/testdeps/tobari.go matches package "testing/internal/testdeps"
			expectedSuffix := "/" + pkgName + "/tobari.go"
			if strings.HasSuffix(origPath, expectedSuffix) {
				// Only add if not already in args (overlay might have already added it)
				if !existingFiles[newPath] {
					newFiles = append(newFiles, newPath)
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
			continue
		}
		if newPath, ok := replace[absArg]; ok {
			// Replace with the overlay path if not already replaced
			if args[i] != newPath {
				args[i] = newPath
			}
		}
	}

	// Add new files to the compile arguments
	return append(args, newFiles...)
}

func getPkgNameFromArgs(args []string) string {
	for i := 0; i < len(args); i++ {
		if args[i] == "-p" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
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
