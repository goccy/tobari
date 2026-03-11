package tool

import (
	"context"
	"fmt"
)

func handleLink(ctx context.Context, toolPath string, args []string, embedCode bool) error {
	importCfgPath := getImportcfgPathFromArgs(args)
	if importCfgPath == "" {
		runCommand(toolPath, args)
		return nil
	}

	// Try to load cached tobari packages. The cache is written during the
	// compile phase of the main package, keyed by the runtime package's
	// export filename, which uniquely identifies the build configuration
	// (including flags like -trimpath, -race, etc.).
	if pkgs, ok := loadTobariPkgsCache(importCfgPath); ok {
		if err := overwriteImportcfg(importCfgPath, pkgs); err != nil {
			return fmt.Errorf("failed to update importcfg: %w", err)
		}
	} else {
		// Cache miss: fall back to building tobari packages without flag
		// information. This path is hit only if the compile phase was
		// skipped entirely AND no prior cache exists.
		pkgs, err := getTobariPkgs(args, embedCode, false, false)
		if err != nil {
			return err
		}
		if err := overwriteImportcfg(importCfgPath, pkgs); err != nil {
			return fmt.Errorf("failed to update importcfg: %w", err)
		}
	}

	runCommand(toolPath, args)
	return nil
}
