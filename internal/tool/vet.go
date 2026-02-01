package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

func handleVet(ctx context.Context, toolPath string, args []string) error {
	if err := replaceVetCfgIfNeeded(args); err != nil {
		return err
	}
	runCommand(toolPath, args)
	return nil
}

func replaceVetCfgIfNeeded(args []string) error {
	for _, arg := range args {
		if !strings.HasSuffix(arg, "vet.cfg") {
			continue
		}

		vetcfg, err := os.ReadFile(arg)
		if err != nil {
			return err
		}
		var m map[string]any
		if err := json.Unmarshal(vetcfg, &m); err != nil {
			return err
		}
		importMap, exists := m["ImportMap"]
		if !exists {
			return fmt.Errorf("failed to get ImportMap section from vet.cfg: %q", vetcfg)
		}
		typedImportMap, ok := importMap.(map[string]any)
		if !ok {
			return fmt.Errorf("failed to get ImportMap as map[string]string type: %q", vetcfg)
		}
		typedImportMap["runtime"] = "runtime"
		typedImportMap["unsafe"] = "unsafe"

		pkgFileMap, exists := m["PackageFile"]
		if !exists {
			return fmt.Errorf("failed to get PackageFile section from vet.cfg: %q", vetcfg)
		}
		typedPkgFileMap, ok := pkgFileMap.(map[string]any)
		if !ok {
			return fmt.Errorf("failed to get PackageFile as map[string]string type: %q", vetcfg)
		}
		if _, exists := typedPkgFileMap["runtime"]; !exists {
			pkgPath, err := readRuntimePkg()
			if err != nil {
				// If runtime_pkg.txt doesn't exist (e.g., runtime is cached),
				// skip adding runtime to vet.cfg - vet will find it from cache
				return nil
			}
			typedPkgFileMap["runtime"] = pkgPath
		}

		b, err := json.Marshal(m)
		if err != nil {
			return fmt.Errorf("failed to encode vet.cfg: %w", err)
		}
		if err := os.WriteFile(arg, b, 0o600); err != nil {
			return fmt.Errorf("failed to rewrite vet.cfg: %w", err)
		}
	}
	return nil
}
