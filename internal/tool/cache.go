package tool

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/goccy/tobari/internal/utils"
)

const tobariPkgsCacheFile = ".tobari-pkgs"

// getRuntimeExportPath parses an importcfg file and returns the export path
// for the runtime package.
func getRuntimeExportPath(importcfgPath string) string {
	f, err := os.Open(importcfgPath)
	if err != nil {
		return ""
	}
	defer func() {
		_ = f.Close()
	}()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "packagefile runtime=") {
			return strings.TrimPrefix(line, "packagefile runtime=")
		}
	}
	return ""
}

// saveTobariPkgsCache writes the tobari package map to cache locations keyed by
// the runtime package's export file. This enables the link phase to find the
// correct tobari packages without knowing which build flags were used.
//
// Two cache locations are written:
//  1. $TMPDIR/tobari/cache/<runtime_cache_filename>.json
//     Used when the link phase references cached packages from the Go build cache.
//     The runtime export filename in Go's build cache is the SHA256 of the file
//     content, so it uniquely identifies the build configuration.
//  2. <dir_of_runtime_work_path>/.tobari-pkgs
//     Used within the same build where compile and link share the same $WORK directory.
func saveTobariPkgsCache(compileImportcfgPath string, pkgs map[string]string) error {
	// Get runtime's path from the compile importcfg (this is a $WORK/bNN/_pkg_.a path)
	runtimeWorkPath := getRuntimeExportPath(compileImportcfgPath)
	if runtimeWorkPath == "" {
		return nil
	}

	// Get runtime's cache path from the pkgs map (GoListDepsExport result)
	runtimeCachePath, ok := pkgs["runtime"]
	if !ok {
		return nil
	}

	data, err := json.Marshal(pkgs)
	if err != nil {
		return fmt.Errorf("failed to marshal tobari pkgs cache: %w", err)
	}

	// Write to cache/<runtime_cache_filename>.json
	cacheDir := filepath.Join(utils.TobariTempDir(), "cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return fmt.Errorf("failed to create cache dir: %w", err)
	}
	cacheFile := filepath.Join(cacheDir, filepath.Base(runtimeCachePath)+".json")
	_ = os.WriteFile(cacheFile, data, 0o600)

	// Write to <dir_of_runtime_work_path>/.tobari-pkgs
	localFile := filepath.Join(filepath.Dir(runtimeWorkPath), tobariPkgsCacheFile)
	_ = os.WriteFile(localFile, data, 0o600)

	return nil
}

// loadTobariPkgsCache attempts to load the cached tobari package map using the
// runtime package's path from the link importcfg.
//
// It tries two cache locations:
//  1. $TMPDIR/tobari/cache/<filename_of_runtime>.json — hits when the link phase
//     references cached packages from Go's build cache.
//  2. <dir_of_runtime>/.tobari-pkgs — hits within the same build where compile
//     and link share the same $WORK directory.
func loadTobariPkgsCache(linkImportcfgPath string) (map[string]string, bool) {
	runtimePath := getRuntimeExportPath(linkImportcfgPath)
	if runtimePath == "" {
		return nil, false
	}

	// Try cache/<filename>.json first
	cacheFile := filepath.Join(utils.TobariTempDir(), "cache", filepath.Base(runtimePath)+".json")
	if pkgs, ok := readAndValidatePkgsCache(cacheFile); ok {
		return pkgs, true
	}

	// Try <dir_of_runtime>/.tobari-pkgs
	localFile := filepath.Join(filepath.Dir(runtimePath), tobariPkgsCacheFile)
	if pkgs, ok := readAndValidatePkgsCache(localFile); ok {
		return pkgs, true
	}

	return nil, false
}

func readAndValidatePkgsCache(path string) (map[string]string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}

	var pkgs map[string]string
	if err := json.Unmarshal(data, &pkgs); err != nil {
		return nil, false
	}

	// Validate: check that the tobari package file still exists
	tobariPath, ok := pkgs["github.com/goccy/tobari"]
	if !ok {
		return nil, false
	}
	if _, err := os.Stat(tobariPath); err != nil {
		return nil, false
	}

	return pkgs, true
}
