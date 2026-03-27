package tool

import (
	"context"
	"fmt"
)

func handleLink(ctx context.Context, toolPath string, args []string, opts BuildOpts) error {
	importCfgPath := getImportcfgPathFromArgs(args)
	if importCfgPath == "" {
		runCommand(toolPath, args)
		return nil
	}

	// Try to load cached tobari packages. The cache is written during the
	// compile phase of the main package, keyed by the runtime package's
	// export filename, which uniquely identifies the build configuration
	// (including flags like -trimpath, -race, -tags, etc.).
	if pkgs, ok := loadTobariPkgsCache(importCfgPath); ok {
		if err := overwriteImportcfg(importCfgPath, pkgs); err != nil {
			return fmt.Errorf("failed to update importcfg: %w", err)
		}
	} else {
		// Cache miss: fall back to building tobari packages without flag
		// information. This path is hit only if the compile phase was
		// skipped entirely AND no prior cache exists.
		fallbackOpts := BuildOpts{
			EmbedCode: opts.EmbedCode,
			BuildTags: opts.BuildTags,
		}
		pkgs, err := getTobariPkgs(args, fallbackOpts)
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
