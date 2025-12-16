package tool

import (
	"context"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

func handleCompile(ctx context.Context, toolPath string, args []string) error {
	args, err := filterCoveragecfg(args)
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

// In Tobari, the AST of the files targeted for coverage measurement is manipulated,
// and the GID() and PGID() functions—dynamically added to the runtime package using the overlay option—are used to measure the Goroutine ID.
// However, if the original file being measured does not import the runtime package, the importcfg file will not contain an archive file path entry for the runtime package,
// resulting in a compilation failure due to unresolved references to the runtime package.
// To prevent this, a reference to the runtime package is dynamically added to the importcfg file.
// Since the runtime package is always built in advance, its package file path is stored and used to resolve the reference.
func addRuntimePkgIfNeeded(importcfgPath string) error {
	importcfg, err := os.ReadFile(importcfgPath)
	if err != nil {
		return fmt.Errorf("failed to read importcfg path %s: %w", importcfgPath, err)
	}
	if strings.Contains(string(importcfg), "packagefile runtime=") {
		// importcfg already has runtime package definition.
		return nil
	}
	archiveFilePath, err := readRuntimePkg()
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
