package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/goccy/tobari/internal/overlay"
	"github.com/goccy/tobari/internal/utils"
)

func handleCompile(ctx context.Context, toolPath string, args []string, embedCode bool) error {
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
	// For main package, add import of github.com/goccy/tobari so that
	// the go command includes tobari and all its transitive deps
	// (including archive/tar, compress/gzip) in the link importcfg.
	// This must happen before addTobariPkgsToImportcfgFromCompileOptions
	// so that the import is detected and tobari is added to importcfg.
	if pkgName == "main" {
		goFile, err := generateTobariImportFile()
		if err != nil {
			return err
		}
		args = append(args, goFile)
	}
	if embedCode {
		var err error
		args, err = handleEmbedCodeForCompile(args)
		if err != nil {
			return err
		}
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

// generateTobariImportFile creates a Go file that imports github.com/goccy/tobari.
// When added to the main package compile args, this ensures the go command
// resolves tobari and all its transitive dependencies at link time.
func generateTobariImportFile() (string, error) {
	dir := filepath.Join(utils.TobariTempDir(), "embed_gen")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create embed_gen dir: %w", err)
	}
	src := `package main

import _ "github.com/goccy/tobari"
`
	path := filepath.Join(dir, "_tobari_import.go")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		return "", fmt.Errorf("failed to write tobari import file: %w", err)
	}
	return path, nil
}

// handleEmbedCodeForCompile handles embed.FS support for covervars.go.
// Since the go command generates embedcfg before the cover tool runs,
// it does not detect //go:embed directives in covervars.go (generated by cover).
// This function creates/merges the embedcfg and adds "embed" to importcfg.
func handleEmbedCodeForCompile(args []string) ([]string, error) {
	tobariFiles, err := findTobariEmbedFiles(args)
	if err != nil {
		return args, err
	}
	if len(tobariFiles) == 0 {
		return args, nil
	}

	// Add "embed" package to importcfg
	if err := addEmbedToImportcfg(args); err != nil {
		return args, err
	}

	// Create or merge embedcfg
	args, err = createOrMergeEmbedcfg(args, tobariFiles)
	if err != nil {
		return args, err
	}

	return args, nil
}

// findTobariEmbedFiles scans directories containing .go files in compile args
// for .go.tobari files that need to be embedded.
func findTobariEmbedFiles(args []string) ([]string, error) {
	dirs := make(map[string]struct{})
	for _, arg := range args {
		if filepath.Ext(arg) == ".go" {
			dir := filepath.Dir(arg)
			if dir == "" {
				dir = "."
			}
			dirs[dir] = struct{}{}
		}
	}

	var tobariFiles []string
	for dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".go.tobari") {
				tobariFiles = append(tobariFiles, filepath.Join(dir, e.Name()))
			}
		}
	}
	return tobariFiles, nil
}

// embedcfg represents the Go compiler's embed configuration format.
type embedcfg struct {
	Patterns map[string][]string `json:"Patterns"`
	Files    map[string]string   `json:"Files"`
}

// createOrMergeEmbedcfg creates a new embedcfg or merges into an existing one.
func createOrMergeEmbedcfg(args []string, tobariFiles []string) ([]string, error) {
	// Build our embed entries
	var names []string
	files := make(map[string]string)
	for _, f := range tobariFiles {
		absPath, err := filepath.Abs(f)
		if err != nil {
			return args, fmt.Errorf("failed to get absolute path for %s: %w", f, err)
		}
		base := filepath.Base(f)
		names = append(names, base)
		files[base] = absPath
	}

	// Check if -embedcfg already exists in args
	existingPath := ""
	for _, arg := range args {
		if strings.HasPrefix(arg, "-embedcfg=") {
			existingPath = strings.TrimPrefix(arg, "-embedcfg=")
			break
		}
	}

	var cfg embedcfg
	if existingPath != "" {
		// Merge into existing embedcfg
		data, err := os.ReadFile(existingPath)
		if err != nil {
			return args, fmt.Errorf("failed to read existing embedcfg %s: %w", existingPath, err)
		}
		if err := json.Unmarshal(data, &cfg); err != nil {
			return args, fmt.Errorf("failed to parse existing embedcfg: %w", err)
		}
		cfg.Patterns["*.go.tobari"] = names
		for k, v := range files {
			cfg.Files[k] = v
		}
		data, err = json.Marshal(cfg)
		if err != nil {
			return args, fmt.Errorf("failed to marshal merged embedcfg: %w", err)
		}
		if err := os.WriteFile(existingPath, data, 0o644); err != nil {
			return args, fmt.Errorf("failed to write merged embedcfg: %w", err)
		}
		return args, nil
	}

	// Create new embedcfg
	cfg = embedcfg{
		Patterns: map[string][]string{"*.go.tobari": names},
		Files:    files,
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return args, fmt.Errorf("failed to marshal embedcfg: %w", err)
	}

	// Write to a temp file next to the source files
	dir := filepath.Dir(tobariFiles[0])
	embedcfgPath := filepath.Join(dir, "tobari_embedcfg.json")
	if err := os.WriteFile(embedcfgPath, data, 0o644); err != nil {
		return args, fmt.Errorf("failed to write embedcfg: %w", err)
	}

	// Insert -embedcfg before the first .go file, because the Go compiler's
	// flag parser stops at the first non-flag argument (source files).
	embedcfgFlag := "-embedcfg=" + embedcfgPath
	for i, arg := range args {
		if filepath.Ext(arg) == ".go" {
			newArgs := make([]string, 0, len(args)+1)
			newArgs = append(newArgs, args[:i]...)
			newArgs = append(newArgs, embedcfgFlag)
			newArgs = append(newArgs, args[i:]...)
			return newArgs, nil
		}
	}
	// Fallback: append at end
	args = append(args, embedcfgFlag)
	return args, nil
}

// addEmbedToImportcfg adds the "embed" package to importcfg if not already present.
func addEmbedToImportcfg(args []string) error {
	importCfgPath := getImportcfgPathFromArgs(args)
	if importCfgPath == "" {
		return nil
	}

	data, err := os.ReadFile(importCfgPath)
	if err != nil {
		return fmt.Errorf("failed to read importcfg: %w", err)
	}

	if strings.Contains(string(data), "packagefile embed=") {
		return nil
	}

	exportPaths, err := utils.GoListExportMap([]string{"embed"})
	if err != nil {
		return fmt.Errorf("failed to get embed package export path: %w", err)
	}

	embedExport, ok := exportPaths["embed"]
	if !ok {
		return fmt.Errorf("embed package not found in go list output")
	}

	newContent := fmt.Sprintf("packagefile embed=%s\n", embedExport) + string(data)
	if err := os.WriteFile(importCfgPath, []byte(newContent), 0o644); err != nil {
		return fmt.Errorf("failed to write updated importcfg with embed: %w", err)
	}
	return nil
}
