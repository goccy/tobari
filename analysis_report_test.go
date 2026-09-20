package tobari_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/goccy/tobari/internal/cover"
)

// TestAnalysisReport verifies that building a main package leaves a report of
// what the dependency analysis cost and of each package's role in it.
//
// testdata/thirdparty passes a coverage-target function to extlib, which calls
// it back, so extlib is on a call path between coverage targets; strconv is
// kept out of the analysis by --exclude-analysis.
func TestAnalysisReport(t *testing.T) {
	ctx := t.Context()
	tobariBin := filepath.Join(t.TempDir(), "tobari")
	if out, err := exec.CommandContext(ctx, "go", "build", "-o", tobariBin, "./cmd/tobari").CombinedOutput(); err != nil {
		t.Fatalf("failed to build tobari: %s: %v", string(out), err)
	}

	coverDir := t.TempDir()
	cmd := exec.CommandContext(ctx, "go", "build", "-cover",
		"-toolexec="+tobariBin+" --exclude-analysis=strconv",
		"-o", filepath.Join(t.TempDir(), "app"), ".")
	cmd.Dir = "testdata/thirdparty"
	cmd.Env = append(os.Environ(),
		"GOCACHE="+t.TempDir(),
		"TMPDIR="+t.TempDir(),
		"TOBARI_COVERDIR="+coverDir,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %s: %v", string(out), err)
	}

	reports, err := cover.ReadAnalysisReports(filepath.Join(coverDir, "tobari", "analysis"))
	if err != nil {
		t.Fatal(err)
	}
	if len(reports) != 1 {
		t.Fatalf("expected one report, got %d", len(reports))
	}
	report := reports[0]
	if report.Target != "example.com/thirdparty" {
		t.Errorf("target: got %q", report.Target)
	}

	stages := make(map[string]float64)
	for _, stage := range report.Stages {
		stages[stage.Name] = stage.Seconds
	}
	for _, name := range []string{"go_list", "load", "ssa_create", "ssa_build", "rta", "vta", "followable_callees", "frontiers", "dependencies"} {
		if _, ok := stages[name]; !ok {
			t.Errorf("stage %q is missing (got %v)", name, report.Stages)
		}
	}

	packages := make(map[string]*cover.AnalysisPackage)
	for _, pkg := range report.Packages {
		packages[pkg.Package] = pkg
	}
	type role struct {
		Module                    string
		Required, OnPath, Skipped bool
	}
	for pkgPath, want := range map[string]role{
		"example.com/thirdparty":         {Module: "example.com/thirdparty", Required: true},
		"example.com/thirdparty/handler": {Module: "example.com/thirdparty", Required: true},
		"example.com/extlib":             {Module: "example.com/extlib", OnPath: true},
		"strconv":                        {Module: "std", Skipped: true},
		"unicode/utf8":                   {Module: "std"},
	} {
		pkg, ok := packages[pkgPath]
		if !ok {
			t.Errorf("package %q is missing", pkgPath)
			continue
		}
		if got := (role{Module: pkg.Module, Required: pkg.Required, OnPath: pkg.OnPath, Skipped: pkg.Skipped}); got != want {
			t.Errorf("package %q: got %+v, want %+v", pkgPath, got, want)
		}
		if built := pkg.SSABuildSeconds > 0; built == want.Skipped {
			t.Errorf("package %q: skipped=%v but SSA build time is %v", pkgPath, want.Skipped, pkg.SSABuildSeconds)
		}
	}
	if pkg := packages["example.com/extlib"]; pkg != nil && (pkg.ReachableFunctions == 0 || pkg.CallEdges == 0) {
		t.Errorf("extlib has no share of the call graph: %+v", pkg)
	}
}
