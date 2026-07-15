package tool

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/mod/modfile"

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
	if len(opts.ExcludeAnalysis) != 0 {
		v += " --exclude-analysis=" + strings.Join(opts.ExcludeAnalysis, ",")
	}
	return v, nil
}

func getTobariPkgs(args []string, opts BuildOpts) (map[string]string, error) {
	ver, err := version.Get()
	if err != nil {
		return nil, err
	}
	// Prefer the target module's tobari version over the binary's version.
	// When the tobari binary is built with one version but the target application
	// depends on a different version, using the binary's version would cause
	// fingerprint mismatches at link time because the overlay-modified runtime
	// packages would reference tobari packages at the binary's version while
	// the target links against its own version.
	if targetVer := resolveTargetTobariVersion(); targetVer != nil {
		ver = targetVer
	}
	workDir, err := workDirFromArgs(args)
	if err != nil {
		return nil, err
	}
	pkgs, err := buildPackages(workDir, ver, getLangFromArgs(args), opts)
	if err != nil {
		return nil, fmt.Errorf("failed to build temp module: %w", err)
	}
	return pkgs, nil
}

// workDirFromArgs derives Go's per-build $WORK directory from a toolexec
// command's args. The compile/link tools always receive an
// `-importcfg $WORK/bNNN/importcfg` flag, so $WORK is the importcfg path's
// grandparent. As a defensive fallback (which should not occur in practice),
// it creates an ephemeral temp dir for this single invocation.
func workDirFromArgs(args []string) (string, error) {
	if importCfgPath := getImportcfgPathFromArgs(args); importCfgPath != "" {
		return filepath.Dir(filepath.Dir(importCfgPath)), nil
	}
	dir, err := os.MkdirTemp("", "tobari_workdir_*")
	if err != nil {
		return "", fmt.Errorf("failed to create fallback workdir: %w", err)
	}
	return dir, nil
}

func getImportcfgPathFromArgs(args []string) string {
	for i, arg := range args {
		if arg == "-importcfg" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// importcfgHasTobari reports whether the importcfg file already contains a
// top-level packagefile entry for github.com/goccy/tobari. We anchor the match
// on the `packagefile ` prefix and trailing `=` so that subpackage entries
// (e.g. github.com/goccy/tobari/internal/...) do not produce false positives.
func importcfgHasTobari(importCfgPath string) bool {
	data, err := os.ReadFile(importCfgPath)
	if err != nil {
		return false
	}
	return strings.Contains(string(data), "packagefile github.com/goccy/tobari=")
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

// resolveTargetTobariVersion reads the target module's go.mod to determine
// which version of github.com/goccy/tobari it depends on. Returns nil if
// the target module does not depend on tobari.
func resolveTargetTobariVersion() *version.Version {
	dir, err := os.Getwd()
	if err != nil {
		return nil
	}
	var goModPath string
	for {
		p := filepath.Join(dir, "go.mod")
		if _, err := os.Stat(p); err == nil {
			goModPath = p
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil
		}
		dir = parent
	}

	data, err := os.ReadFile(goModPath)
	if err != nil {
		return nil
	}
	f, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		return nil
	}

	modDir := filepath.Dir(goModPath)

	// Check replace directives first.
	for _, rep := range f.Replace {
		if rep.Old.Path == "github.com/goccy/tobari" {
			if rep.New.Version == "" {
				localPath := rep.New.Path
				if !filepath.IsAbs(localPath) {
					localPath = filepath.Join(modDir, localPath)
				}
				return &version.Version{LocalPath: localPath}
			}
			return &version.Version{Ver: rep.New.Version}
		}
	}

	// Check require directives.
	for _, req := range f.Require {
		if req.Mod.Path == "github.com/goccy/tobari" {
			return &version.Version{Ver: req.Mod.Version}
		}
	}

	return nil
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
