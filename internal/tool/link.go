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
	// export filename — which uniquely identifies the build configuration
	// (including flags like -trimpath, -race, -tags, etc.) — plus the tobari
	// version this build resolves to, so builds that pin tobari differently
	// never pick up each other's entries.
	ver, err := effectiveTobariVersion()
	if err != nil {
		return err
	}
	if pkgs, ok := loadTobariPkgsCache(importCfgPath, ver.ID()); ok {
		if err := overwriteImportcfg(importCfgPath, pkgs); err != nil {
			return fmt.Errorf("failed to update importcfg: %w", err)
		}
	} else if !importcfgHasTobari(importCfgPath) {
		// Cache miss: fall back to building tobari packages without flag
		// information. This path is hit only if the compile phase was
		// skipped entirely AND no prior cache exists.
		// Skip when tobari is already in importcfg (user code directly
		// imports tobari) — same reasoning as the compile phase early
		// return: there is nothing for us to add.
		fallbackOpts := BuildOpts{
			EmbedCode: opts.EmbedCode,
			BuildTags: opts.BuildTags,
		}
		pkgs, err := getTobariPkgs(args, fallbackOpts, ver)
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
