package tool

import (
	"context"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/goccy/tobari/internal/cover"
	"github.com/goccy/tobari/internal/overlay"
	"github.com/goccy/tobari/internal/utils"
)

//go:embed templates/mainhook.go.tmpl
var mainHookTmplFS embed.FS

var mainHookTmpl = template.Must(template.ParseFS(mainHookTmplFS, "templates/mainhook.go.tmpl"))

func handleCompile(ctx context.Context, toolPath string, args []string, opts BuildOpts) error {
	// Detect trimpath and race from compiler args.
	opts.Trimpath = hasExplicitTrimpath(args)
	opts.Race = hasRaceFlag(args)
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

		// Render overlay for this package only.
		// Pass counter mode so testdeps overlay uses the correct coverage mode.
		counterMode := "set"
		if opts.Race {
			counterMode = "atomic"
		}
		pkg, err := overlay.RenderPackage(def, sourceFiles, map[string]string{
			"counterMode": counterMode,
		})
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

		// Add missing imports to importcfg.
		// Pass toolexec and trimpath to ensure packages are compiled with the
		// same build cache entries as the outer build, preventing fingerprint
		// mismatches at link time.
		toolexec, err := tobariToolexec(opts)
		if err != nil {
			return err
		}
		if err := addMissingImportsToImportcfg(args, pkg.Imports, toolexec, opts); err != nil {
			return err
		}
	}

	// Always inject tobari import for main package.
	// This ensures tobari packages are in the importcfg, which is required
	// because overlay-modified packages (runtime, testing) reference tobari symbols
	// via //go:linkname. Without this import, projects that don't directly import
	// tobari would fail with relocation errors at link time.
	// When embed-code is enabled, also inject the source extraction hook.
	if pkgName == "main" {
		// Collect source files from args for ReadSuppDeps to search
		// the per-package $WORK directory (testmain builds).
		var mainGoFiles []string
		for _, arg := range args {
			if filepath.Ext(arg) == ".go" {
				mainGoFiles = append(mainGoFiles, arg)
			}
		}
		suppDeps, err := cover.ReadSuppDeps(mainGoFiles)
		if err != nil {
			return fmt.Errorf("failed to read supplementary deps: %w", err)
		}
		hookFile, err := generateMainHook(opts.EmbedCode, suppDeps)
		if err != nil {
			return fmt.Errorf("failed to generate main hook: %w", err)
		}
		args = append(args, hookFile)

		// Build tobari packages and cache the result for the link phase.
		// The cache is keyed by the runtime package's export filename, which
		// uniquely identifies the build configuration (flags like -trimpath,
		// -race, etc. all change the cache key). This allows the link phase
		// to find the correctly-built tobari packages without needing to
		// detect individual flags.
		//
		// Skip entirely when tobari is already in the importcfg — that means
		// the user's code directly imports tobari, so the Go build has
		// already resolved and placed the tobari entries into importcfg, and
		// running `go list -deps -export` (which is slow and creates the
		// per-build app dir) would be wasted work.
		if importCfgPath := getImportcfgPathFromArgs(args); importCfgPath != "" && !importcfgHasTobari(importCfgPath) {
			ver, err := effectiveTobariVersion()
			if err != nil {
				return err
			}
			pkgs, err := getTobariPkgs(args, opts, ver)
			if err != nil {
				return fmt.Errorf("failed to build tobari packages: %w", err)
			}
			if err := saveTobariPkgsCache(importCfgPath, pkgs, ver.ID()); err != nil {
				return err
			}
			if err := overwriteImportcfg(importCfgPath, pkgs); err != nil {
				return fmt.Errorf("failed to update importcfg: %w", err)
			}
		}
	}

	args, err := filterCoveragecfg(args)
	if err != nil {
		return err
	}
	if err := addTobariPkgsToImportcfgFromCompileOptions(args, opts); err != nil {
		return err
	}
	runCommand(toolPath, args)
	return nil
}

func generateMainHook(embedCode bool, suppDeps string) (string, error) {
	f, err := os.CreateTemp("", utils.TmpMainHookPattern)
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	if err := mainHookTmpl.Execute(f, struct {
		SuppDeps  string
		EmbedCode bool
	}{
		SuppDeps:  fmt.Sprintf("%q", suppDeps),
		EmbedCode: embedCode,
	}); err != nil {
		_ = f.Close()
		return "", fmt.Errorf("failed to write main hook: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return f.Name(), nil
}

// addTobariPkgsToImportcfgFromCompileOptions
// dynamically add an import statement for github.com/goccy/tobari in testmain.go,
// there may be cases where it doesn't exist in the importcfg as is.
// In such cases, if the target test uses github.com/goccy/tobari, linking is possible;
// however, if it doesn't use it, a tobari package must be created dynamically and its path specified.
func addTobariPkgsToImportcfgFromCompileOptions(args []string, opts BuildOpts) error {
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

	ver, err := effectiveTobariVersion()
	if err != nil {
		return err
	}
	pkgs, err := getTobariPkgs(args, opts, ver)
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
// toolexec and trimpath are passed to GoListExportMap so that packages use
// the same build cache entries as the outer build.
func addMissingImportsToImportcfg(args []string, imports []string, toolexec string, opts BuildOpts) error {
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
	exportPaths, err := utils.GoListExportMap(missingImports, utils.GoListOpts{
		Toolexec:  toolexec,
		Trimpath:  opts.Trimpath,
		Race:      opts.Race,
		BuildTags: opts.BuildTags,
	})
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
