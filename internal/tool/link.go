package tool

import (
	"context"
	"fmt"
)

func handleLink(ctx context.Context, toolPath string, args []string) error {
	importCfgPath := getImportcfgPathFromArgs(args)
	if importCfgPath == "" {
		runCommand(toolPath, args)
		return nil
	}
	if err := addTobariPkgsToImportcfgFromLinkOptions(importCfgPath, args); err != nil {
		return err
	}
	runCommand(toolPath, args)
	return nil
}

func addTobariPkgsToImportcfgFromLinkOptions(importCfgPath string, args []string) error {
	pkgs, err := getTobariPkgs(args)
	if err != nil {
		return err
	}
	if err := overwriteImportcfg(importCfgPath, pkgs); err != nil {
		return fmt.Errorf("failed to update importcfg: %w", err)
	}
	return nil
}
