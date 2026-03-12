package tool

import (
	"fmt"
	"os"
	"strings"

	"github.com/goccy/tobari/internal/version"
)

func tobariToolexec(opts BuildOpts) (string, error) {
	tobariPath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("failed to get tobari binary path: %w", err)
	}
	v := tobariPath
	if opts.EmbedCode {
		v += " --embed-code"
	}
	if opts.BuildTags != "" {
		v += " --build-tags=" + opts.BuildTags
	}
	return v, nil
}

func getTobariPkgs(args []string, opts BuildOpts) (map[string]string, error) {
	ver, err := version.Get()
	if err != nil {
		return nil, err
	}
	pkgs, err := buildPackages(ver, getLangFromArgs(args), opts)
	if err != nil {
		return nil, fmt.Errorf("failed to build temp module: %w", err)
	}
	return pkgs, nil
}

func getImportcfgPathFromArgs(args []string) string {
	for i, arg := range args {
		if arg == "-importcfg" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// hasExplicitTrimpath detects whether the user has passed -trimpath to go build/test.
// The Go compiler always receives a -trimpath flag with a single directory for internal
// normalization. When the user explicitly passes -trimpath, the value contains
// ';'-separated directories instead.
func hasExplicitTrimpath(args []string) bool {
	for i, arg := range args {
		if arg == "-trimpath" && i+1 < len(args) && strings.Contains(args[i+1], ";") {
			return true
		}
	}
	return false
}

// hasRaceFlag detects whether the outer build is using -race.
// When `go build -race` or `go test -race` is used, Go passes -race to the compiler.
func hasRaceFlag(args []string) bool {
	for _, arg := range args {
		if arg == "-race" {
			return true
		}
	}
	return false
}

func getLangFromArgs(args []string) string {
	for _, arg := range args {
		if strings.HasPrefix(arg, "-lang=") {
			return strings.TrimPrefix(arg, "-lang=")
		}
	}
	return ""
}

func overwriteImportcfg(importCfgPath string, pkgs map[string]string) error {
	data, err := os.ReadFile(importCfgPath)
	if err != nil {
		return fmt.Errorf("failed to read importcfg: %w", err)
	}

	content := string(data)

	if strings.Contains(content, "github.com/goccy/tobari") {
		return nil
	}

	var newEntries strings.Builder
	for importPath, pkgPath := range pkgs {
		// Skip packages that already exist in the importcfg
		if strings.Contains(content, "packagefile "+importPath+"=") {
			continue
		}
		fmt.Fprintf(&newEntries, "packagefile %s=%s\n", importPath, pkgPath)
	}

	if newEntries.Len() == 0 {
		return nil
	}

	content = newEntries.String() + content
	if err := os.WriteFile(importCfgPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("failed to write updated importcfg: %w", err)
	}
	return nil
}
