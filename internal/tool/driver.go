// GOPACKAGESDRIVER implementation for tobari.
//
// When CreateMainDeps calls packages.Load, it sets GOPACKAGESDRIVER to
// the tobari binary with TOBARI_PACKAGES_DRIVER=1. packages.Load then
// invokes tobari as an external driver process instead of calling go list.
//
// The driver reads pre-computed go list -deps -json output (saved to a
// temp file by CreateMainDeps) and converts it to a DriverResponse.
package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/goccy/tobari/internal/utils"
)

// goListPackage represents a single package from go list -json output.
type goListPackage struct {
	Dir             string
	ImportPath      string
	Name            string
	ForTest         string
	GoFiles         []string
	CompiledGoFiles []string
	Imports         []string
	ImportMap       map[string]string
}

// HandlePackagesDriver handles the GOPACKAGESDRIVER protocol.
func HandlePackagesDriver(ctx context.Context, patterns []string) error {
	var req packages.DriverRequest
	if err := json.NewDecoder(os.Stdin).Decode(&req); err != nil {
		return fmt.Errorf("failed to decode driver request: %w", err)
	}

	// Read go list JSON output from temp file.
	var goListPkgs []goListPackage
	if path := os.Getenv(utils.EnvGoListFile); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read go list file: %w", err)
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
		for decoder.More() {
			var pkg goListPackage
			if err := decoder.Decode(&pkg); err != nil {
				return fmt.Errorf("failed to decode go list entry: %w", err)
			}
			goListPkgs = append(goListPkgs, pkg)
		}
	}

	// Read cover package paths from temp file.
	coverPkgPaths := make(map[string]struct{})
	if path := os.Getenv(utils.EnvCoverPkgPathsFile); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read cover pkg paths file: %w", err)
		}
		var paths []string
		if err := json.Unmarshal(data, &paths); err != nil {
			return fmt.Errorf("failed to decode cover pkg paths: %w", err)
		}
		for _, p := range paths {
			coverPkgPaths[p] = struct{}{}
		}
	}

	// Build DriverResponse from go list output.
	pkgMap := make(map[string]*packages.Package)

	pkgMap["unsafe"] = &packages.Package{
		ID:      "unsafe",
		Name:    "unsafe",
		PkgPath: "unsafe",
	}

	for _, glp := range goListPkgs {
		// Convert relative GoFiles to absolute paths using Dir.
		absGoFiles := make([]string, 0, len(glp.GoFiles))
		for _, f := range glp.GoFiles {
			if filepath.IsAbs(f) {
				absGoFiles = append(absGoFiles, f)
			} else {
				absGoFiles = append(absGoFiles, filepath.Join(glp.Dir, f))
			}
		}

		sourceFiles := absGoFiles

		// Build imports map. ImportMap maps source-level import paths to resolved
		// package IDs (used for test variants like "pkg [pkg.test]").
		imports := make(map[string]*packages.Package)
		importMapValues := make(map[string]struct{}, len(glp.ImportMap))
		for srcPath, resolvedPath := range glp.ImportMap {
			imports[srcPath] = &packages.Package{ID: resolvedPath}
			importMapValues[resolvedPath] = struct{}{}
		}
		for _, imp := range glp.Imports {
			if _, isMapped := importMapValues[imp]; isMapped {
				continue
			}
			if _, exists := imports[imp]; !exists {
				imports[imp] = &packages.Package{ID: imp}
			}
		}
		imports["unsafe"] = &packages.Package{ID: "unsafe"}

		pkgMap[glp.ImportPath] = &packages.Package{
			ID:              glp.ImportPath,
			Name:            glp.Name,
			PkgPath:         glp.ImportPath,
			GoFiles:         sourceFiles,
			CompiledGoFiles: sourceFiles,
			Imports:         imports,
		}
	}

	// Build roots: main package, cover targets, and test packages.
	rootSet := make(map[string]struct{})
	// The main package must be identified by its package name, not by its
	// import path: `go list` reports a main package's ImportPath as its module
	// path (e.g. "example.com/app"), never the literal "main". Without this the
	// main package is not loaded at all, so RTA cannot trace from main.
	for _, glp := range goListPkgs {
		if glp.Name == "main" {
			rootSet[glp.ImportPath] = struct{}{}
		}
	}
	for pkgPath := range coverPkgPaths {
		if _, ok := pkgMap[pkgPath]; ok {
			rootSet[pkgPath] = struct{}{}
		}
	}
	// Include test variant and test binary packages as roots so that
	// packages.Load can resolve test imports (e.g., "testing").
	for id := range pkgMap {
		if strings.HasSuffix(id, ".test") || strings.Contains(id, " [") {
			rootSet[id] = struct{}{}
		}
	}
	roots := make([]string, 0, len(rootSet))
	for id := range rootSet {
		roots = append(roots, id)
	}
	// Deterministic order: packages.Load's roots feed RTA root ordering, and
	// callgraph.New(roots[0]) treats the first root specially.
	sort.Strings(roots)

	var pkgList []*packages.Package
	for _, dp := range pkgMap {
		pkgList = append(pkgList, dp)
	}

	resp := packages.DriverResponse{
		Compiler: "gc",
		Arch:     runtime.GOARCH,
		Roots:    roots,
		Packages: pkgList,
	}

	return json.NewEncoder(os.Stdout).Encode(&resp)
}
