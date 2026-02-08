package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

func handleVet(ctx context.Context, toolPath string, args []string) error {
	if err := fixVetCfgImportMap(args); err != nil {
		return err
	}
	runCommand(toolPath, args)
	return nil
}

// fixVetCfgImportMap ensures that "unsafe" is correctly mapped in the vet.cfg ImportMap.
// Go's coverage tool generates covervars.go which imports "unsafe", but the ImportMap
// may not include it, causing vet to fail with "can't resolve import" errors.
func fixVetCfgImportMap(args []string) error {
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
			continue
		}
		typedImportMap, ok := importMap.(map[string]any)
		if !ok {
			continue
		}
		typedImportMap["unsafe"] = "unsafe"

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
