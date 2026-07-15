package utils

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Tobari-managed temp file names. All tobari-created temp files use the
// "tobari_" prefix with underscore separators. Files under $WORK/bNNN/ are
// shared between the cover/compile/link phases of the same package; files
// created via os.CreateTemp/os.MkdirTemp are ephemeral per tool invocation.
const (
	// tmpWorkAppDirName is the per-build directory under $WORK that holds
	// a minimal Go module used to resolve tobari package export paths.
	tmpWorkAppDirName = "tobari_app"

	// TmpSuppDepsFile is written by the cover tool under $WORK/bNNN/ and
	// read by the compile tool to inject supplementary dependencies.
	TmpSuppDepsFile = "tobari_suppdeps.json"

	// TmpPkgsCacheFile is written by the compile tool under $WORK/bNNN/
	// and read by the link tool to recover tobari package export paths.
	TmpPkgsCacheFile = "tobari_pkgs.json"

	// TmpMainHookPattern is the os.CreateTemp pattern for the generated
	// tobari_main.go hook file injected into main package compilation.
	TmpMainHookPattern = "tobari_main_*.go"

	// TmpDriverJSONPattern is the os.CreateTemp pattern for GOPACKAGESDRIVER
	// IPC files.
	TmpDriverJSONPattern = "tobari_driver_*.json"

	// TmpCoverprofilePattern is the os.CreateTemp pattern for the intermediate
	// coverprofile used by `tobari html`.
	TmpCoverprofilePattern = "tobari_coverprofile_*.txt"

	// TmpHTMLDirPattern is the os.MkdirTemp pattern used by `tobari html`
	// for its working directory.
	TmpHTMLDirPattern = "tobari_html_*"
)

func GoRoot() (string, error) {
	// check GOROOT environment variable first (set by toolchain switching)
	if root := os.Getenv("GOROOT"); root != "" {
		return root, nil
	}
	cmd, err := exec.LookPath("go")
	if err != nil {
		return "", fmt.Errorf("failed to find go binary path: %w", err)
	}
	out, err := exec.Command(cmd, "env", "GOROOT").Output()
	if err != nil {
		return "", fmt.Errorf("failed to get GOROOT: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func GoPkgPath(pkg string) (string, error) {
	root, err := GoRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "src", pkg), nil
}

func GoPkgFiles(pkgPath string) ([]string, error) {
	entries, err := os.ReadDir(pkgPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory %s: %w", pkgPath, err)
	}
	var ret []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") {
			continue
		}
		ret = append(ret, filepath.Join(pkgPath, name))
	}
	return ret, nil
}

func GoBin() (string, error) {
	root, err := GoRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "bin", "go"), nil
}

func GoVersion() (string, error) {
	bin, err := GoBin()
	if err != nil {
		return "", err
	}
	out, err := exec.Command(bin, "env", "GOVERSION").Output()
	if err != nil {
		return "", fmt.Errorf("failed to get GOVERSION from %s: %w", bin, err)
	}
	return strings.TrimSpace(string(out)), nil
}

func GoModTidy(dir string) error {
	bin, err := GoBin()
	if err != nil {
		return err
	}
	goRoot, err := GoRoot()
	if err != nil {
		return err
	}
	cmd := exec.Command(bin, "mod", "tidy")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOROOT="+goRoot)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to run 'go mod tidy' for app %s: %s: %w", dir, string(out), err)
	}
	return nil
}

// GoListOpts bundles options for go list invocations.
type GoListOpts struct {
	Toolexec  string
	Trimpath  bool
	Race      bool
	BuildTags string
}

// goListArgs returns the common flags derived from the options.
func (o GoListOpts) goListArgs() []string {
	var args []string
	if o.Toolexec != "" {
		args = append(args, "-toolexec="+o.Toolexec)
	}
	if o.Trimpath {
		args = append(args, "-trimpath")
	}
	if o.Race {
		args = append(args, "-race")
	}
	if o.BuildTags != "" {
		args = append(args, "-tags="+o.BuildTags)
	}
	return args
}

// GoListExportMap runs `go list -export -json` for the given packages
// and returns a map of import path -> export file path.
// When toolexec and trimpath are provided, they are passed to `go list`
// so that the compiled packages use the same build cache entries as the
// outer build, ensuring matching fingerprints at link time.
func GoListExportMap(pkgs []string, opts GoListOpts) (map[string]string, error) {
	bin, err := GoBin()
	if err != nil {
		return nil, err
	}
	args := append([]string{"list", "-export", "-json"}, opts.goListArgs()...)
	args = append(args, pkgs...)
	cmd := exec.Command(bin, args...)
	// Strip GOFLAGS to prevent tobari's -toolexec from being inherited,
	// which would cause recursive invocations.
	cmd.Env = FilterGOFLAGSEnvs()
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to run go list: %w", err)
	}
	return parseGoListExportJSON(out)
}

// GoListDepsExport runs `go list -deps -export -json` in the specified directory.
// It strips GOFLAGS and explicitly passes -toolexec to avoid recursive invocations
// and to ensure the inner build uses exactly the specified toolexec command.
//
// When trimpath is true, -trimpath is passed to go list so that Go computes
// the same -trimpath value and action ID as the outer build. This ensures that
// the inner build produces standard library packages in the same build cache
// entries as the outer build, preventing fingerprint mismatches at link time.
func GoListDepsExport(dir string, opts GoListOpts, pkg string) (map[string]string, error) {
	bin, err := GoBin()
	if err != nil {
		return nil, err
	}
	args := append([]string{"list", "-deps", "-export", "-json"}, opts.goListArgs()...)
	args = append(args, pkg)
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	cmd.Env = FilterGOFLAGSEnvs()
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to run go list -export -deps: %w", err)
	}
	return parseGoListExportJSON(out)
}

// GoListDepsJSON runs `go list -deps -json` and returns the raw JSON output.
// Unlike GoListDepsExport, no -export flag is used so no compilation occurs.
// When isTest is true, -test is added to include test dependencies.
func GoListDepsJSON(dir string, isTest bool, pkg string) ([]byte, error) {
	bin, err := GoBin()
	if err != nil {
		return nil, err
	}
	args := []string{"list", "-deps", "-json"}
	if isTest {
		args = append(args, "-test")
	}
	args = append(args, pkg)
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	cmd.Env = FilterGOFLAGSEnvs()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to run go list -deps in %s: %w: %s", dir, err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

func parseGoListExportJSON(data []byte) (map[string]string, error) {
	ret := make(map[string]string)
	decoder := json.NewDecoder(bytes.NewReader(data))
	for decoder.More() {
		var pkg struct {
			ImportPath string
			Export     string
		}
		if err := decoder.Decode(&pkg); err != nil {
			return nil, fmt.Errorf("failed to decode go list output: %w", err)
		}
		if pkg.Export != "" {
			ret[pkg.ImportPath] = pkg.Export
		}
	}
	return ret, nil
}

// FilterGOFLAGSEnvs strips GOFLAGS to prevent -cover/-toolexec from being
// inherited by inner go list invocations, which would cause recursive
// invocations and double coverage instrumentation. Build tags and other
// flags are passed explicitly via function parameters instead.
func FilterGOFLAGSEnvs() []string {
	envs := os.Environ()
	newEnvs := make([]string, 0, len(envs))
	for _, kv := range envs {
		i := strings.IndexByte(kv, '=')
		if kv[:i] == "GOFLAGS" {
			continue
		}
		newEnvs = append(newEnvs, kv)
	}
	return newEnvs
}

// ParsePkgPrefixes splits a comma-separated list of package-path prefixes,
// trimming whitespace and dropping empty entries.
func ParsePkgPrefixes(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// MatchesPkgPrefix reports whether pkgPath equals one of the prefixes or is a
// subpackage of one (prefix + "/").
func MatchesPkgPrefix(pkgPath string, prefixes []string) bool {
	for _, p := range prefixes {
		if pkgPath == p || strings.HasPrefix(pkgPath, p+"/") {
			return true
		}
	}
	return false
}

// ImportsFromSource extracts import packages from Go source.
func ImportsFromSource(src []byte) ([]string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", src, parser.ImportsOnly)
	if err != nil {
		return nil, err
	}

	imports := make([]string, 0, len(file.Imports))
	for _, imp := range file.Imports {
		v, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			return nil, err
		}
		imports = append(imports, v)
	}
	return imports, nil
}

func TobariTempDir() string {
	return filepath.Join(os.TempDir(), "tobari")
}

func OverlayDir() string {
	return filepath.Join(TobariTempDir(), "overlay")
}

// WorkAppDir returns the per-build temporary module directory created
// under Go's $WORK. It is shared across compile/link toolexec invocations
// of the same build and automatically cleaned up by `go build`.
func WorkAppDir(workDir string) string {
	return filepath.Join(workDir, tmpWorkAppDirName)
}

// CoverPkgsDir returns the directory for the global cover package cache.
func CoverPkgsDir() string {
	return filepath.Join(TobariTempDir(), "coverpkgs")
}
