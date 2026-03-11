package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/goccy/tobari/internal/utils"
)

func handleLink(ctx context.Context, toolPath string, args []string, embedCode bool) error {
	importCfgPath := getImportcfgPathFromArgs(args)
	if importCfgPath == "" {
		runCommand(toolPath, args)
		return nil
	}

	// Detect trimpath from marker files written by the compile phase.
	// The linker never receives -trimpath, so we rely on markers.
	//
	// 1. Work-dir marker: written during the current build (compile and link
	//    share the same Go build work directory).
	// 2. Persistent marker: written to $TMPDIR/tobari/.trimpath. Used as a
	//    fallback when the compile phase was cached and no work-dir marker
	//    exists. This is safe because switching the -trimpath flag invalidates
	//    Go's build cache, ensuring compile runs and updates the marker.
	var trimpath bool
	if workDir := getWorkDirFromImportcfg(args); workDir != "" {
		if _, err := os.Stat(filepath.Join(workDir, ".tobari-trimpath")); err == nil {
			trimpath = true
		}
	}
	if !trimpath {
		if _, err := os.Stat(filepath.Join(utils.TobariTempDir(), ".trimpath")); err == nil {
			trimpath = true
		}
	}

	if err := addTobariPkgsToImportcfgFromLinkOptions(importCfgPath, args, embedCode, trimpath); err != nil {
		return err
	}
	runCommand(toolPath, args)
	return nil
}

func addTobariPkgsToImportcfgFromLinkOptions(importCfgPath string, args []string, embedCode bool, trimpath bool) error {
	pkgs, err := getTobariPkgs(args, embedCode, trimpath)
	if err != nil {
		return err
	}
	if err := overwriteImportcfg(importCfgPath, pkgs); err != nil {
		return fmt.Errorf("failed to update importcfg: %w", err)
	}
	return nil
}
