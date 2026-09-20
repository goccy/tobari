package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/goccy/tobari/internal/cover"
)

func testAnalysisReport() *cover.AnalysisReport {
	return &cover.AnalysisReport{
		Target: "example.com/app",
		Stages: []cover.AnalysisStage{
			{Name: "load", Seconds: 5},
			{Name: "ssa_build", Seconds: 10},
			{Name: "rta", Seconds: 30},
			{Name: "vta", Seconds: 70},
		},
		Packages: []*cover.AnalysisPackage{
			{Package: "example.com/app", Module: "example.com/app", SSABuildSeconds: 1, CallEdges: 100, Required: true},
			{Package: "example.com/app/handler", Module: "example.com/app", SSABuildSeconds: 1, CallEdges: 100, Required: true, OnPath: true},
			{Package: "example.com/app.test"},
			{Package: "example.com/bridge", Module: "example.com/bridge", SSABuildSeconds: 2, CallEdges: 200, OnPath: true},
			{Package: "example.com/gen/a", Module: "example.com/gen", SSABuildSeconds: 8, CallEdges: 250},
			{Package: "example.com/gen/b", Module: "example.com/gen", SSABuildSeconds: 8, CallEdges: 250},
			{Package: "example.com/excluded", Module: "example.com/excluded", Skipped: true},
			{Package: "example.com/tiny", Module: "example.com/tiny", CallEdges: 1},
			{Package: "fmt", Module: "std", CallEdges: 99},
		},
	}
}

func TestSummarizeAnalysis(t *testing.T) {
	got := summarizeAnalysis(testAnalysisReport(), 0.01)

	// SSA build: 10s split by the 20s of per-package build time. Call graph:
	// 100s split by the 1000 call edges.
	want := &analysisSummary{
		Target:  "example.com/app",
		Seconds: 115,
		Stages:  testAnalysisReport().Stages,
		Modules: []*moduleSummary{
			{Module: "example.com/gen", Packages: 2, SSABuildSeconds: 16, CallEdges: 500, EstimatedSeconds: 58, Share: 58.0 / 110, Role: roleOffPath},
			{Module: "example.com/app", Packages: 2, SSABuildSeconds: 2, CallEdges: 200, EstimatedSeconds: 21, Share: 21.0 / 110, Role: roleRequired},
			{Module: "example.com/bridge", Packages: 1, SSABuildSeconds: 2, CallEdges: 200, EstimatedSeconds: 21, Share: 21.0 / 110, Role: roleOnPath},
			{Module: "std", Packages: 1, CallEdges: 99, EstimatedSeconds: 9.9, Share: 9.9 / 110, Role: roleOffPath},
			{Module: "example.com/tiny", Packages: 1, CallEdges: 1, EstimatedSeconds: 0.1, Share: 0.1 / 110, Role: roleOffPath},
			{Module: unknownModule, Packages: 1, Role: roleOffPath},
			{Module: "example.com/excluded", Packages: 1, Role: roleSkipped},
		},
		// Off-path and above the threshold. std and the unknown module cannot
		// be named as a prefix; tiny is below the threshold.
		Candidates: []string{"example.com/gen"},
		Verified:   []string{},
	}
	if diff := cmp.Diff(want, got, cmpopts.EquateApprox(0, 1e-9)); diff != "" {
		t.Errorf("summary (-want +got):\n%s", diff)
	}

	if got := summarizeAnalysis(testAnalysisReport(), 0).Candidates; !cmp.Equal(got, []string{"example.com/gen", "example.com/tiny"}) {
		t.Errorf("candidates without a threshold: got %v", got)
	}
}

// TestSummarizeAnalysisVerdicts checks how what the build verified about a
// module shows up: only a module whose analyzed packages all passed is
// verified, and a module with a package that failed is never a candidate.
func TestSummarizeAnalysisVerdicts(t *testing.T) {
	report := &cover.AnalysisReport{
		Target: "example.com/app",
		Stages: []cover.AnalysisStage{{Name: "rta", Seconds: 10}},
		Packages: []*cover.AnalysisPackage{
			{Package: "example.com/app", Module: "example.com/app", CallEdges: 10, Required: true},
			{Package: "example.com/verified/a", Module: "example.com/verified", CallEdges: 10, Verdict: cover.VerdictSkip},
			{Package: "example.com/verified/b", Module: "example.com/verified", CallEdges: 10, Verdict: cover.VerdictSkip},
			{Package: "example.com/partly/a", Module: "example.com/partly", CallEdges: 10, Verdict: cover.VerdictSkip},
			{Package: "example.com/partly/b", Module: "example.com/partly", CallEdges: 10},
			{Package: "example.com/needed/a", Module: "example.com/needed", CallEdges: 10, Verdict: cover.VerdictSkip},
			{Package: "example.com/needed/b", Module: "example.com/needed", CallEdges: 10, Verdict: cover.VerdictKeep},
			{Package: "example.com/learned", Module: "example.com/learned", Skipped: true, Learned: true, Verdict: cover.VerdictSkip},
			{Package: "example.com/excluded", Module: "example.com/excluded", Skipped: true},
		},
	}
	summary := summarizeAnalysis(report, 0)

	roles := make(map[string]moduleRole)
	for _, m := range summary.Modules {
		roles[m.Module] = m.Role
	}
	wantRoles := map[string]moduleRole{
		"example.com/app":      roleRequired,
		"example.com/verified": roleVerified,
		"example.com/partly":   roleOffPath,
		"example.com/needed":   roleNeeded,
		"example.com/learned":  roleLearned,
		"example.com/excluded": roleSkipped,
	}
	if diff := cmp.Diff(wantRoles, roles); diff != "" {
		t.Errorf("roles (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"example.com/partly", "example.com/verified"}, summary.Candidates); diff != "" {
		t.Errorf("candidates (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"example.com/verified"}, summary.Verified); diff != "" {
		t.Errorf("verified (-want +got):\n%s", diff)
	}
}

// writeTestAnalysisReport stores the report the way the cover tool does, in a
// "tobari/analysis" directory under dir.
func writeTestAnalysisReport(t *testing.T, dir string, report *cover.AnalysisReport) {
	t.Helper()
	reportDir := filepath.Join(dir, "tobari", "analysis")
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(reportDir, "report.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAnalysisCmd(t *testing.T) {
	t.Setenv("TOBARI_COVERDIR", "")
	root := t.TempDir()
	writeTestAnalysisReport(t, filepath.Join(root, "cmd", "app"), testAnalysisReport())
	other := testAnalysisReport()
	other.Target = "example.com/app/pkg.test"
	writeTestAnalysisReport(t, filepath.Join(root, "pkg"), other)
	// Reports under hidden directories are somebody else's.
	hidden := testAnalysisReport()
	hidden.Target = "example.com/hidden"
	writeTestAnalysisReport(t, filepath.Join(root, ".cache"), hidden)

	t.Run("text", func(t *testing.T) {
		var stdout bytes.Buffer
		c := &CLI{stdout: &stdout, stderr: &bytes.Buffer{}}
		if err := c.runAnalysisCmd(context.Background(), []string{root}); err != nil {
			t.Fatal(err)
		}
		out := stdout.String()
		for _, want := range []string{
			"example.com/app\n",
			"example.com/app/pkg.test\n",
			"analysis 115.00s (load 5.00s, ssa_build 10.00s, rta 30.00s, vta 70.00s)",
			"-exclude-analysis=example.com/gen\n",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("output does not contain %q:\n%s", want, out)
			}
		}
		if strings.Contains(out, "example.com/hidden") {
			t.Errorf("output contains a report from a hidden directory:\n%s", out)
		}
	})

	t.Run("json", func(t *testing.T) {
		var stdout bytes.Buffer
		c := &CLI{stdout: &stdout, stderr: &bytes.Buffer{}}
		if err := c.runAnalysisCmd(context.Background(), []string{"-json", "-min-share", "0", root}); err != nil {
			t.Fatal(err)
		}
		var got []*analysisSummary
		if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
			t.Fatalf("%v:\n%s", err, stdout.String())
		}
		if len(got) != 2 || got[0].Target != "example.com/app" || got[1].Target != "example.com/app/pkg.test" {
			t.Fatalf("unexpected targets: %s", stdout.String())
		}
		if want := []string{"example.com/gen", "example.com/tiny"}; !cmp.Equal(got[0].Candidates, want) {
			t.Errorf("candidates: got %v, want %v", got[0].Candidates, want)
		}
	})

	t.Run("cover dir is the default place to look", func(t *testing.T) {
		t.Setenv("TOBARI_COVERDIR", filepath.Join(root, "pkg"))
		var stdout bytes.Buffer
		c := &CLI{stdout: &stdout, stderr: &bytes.Buffer{}}
		if err := c.runAnalysisCmd(context.Background(), nil); err != nil {
			t.Fatal(err)
		}
		if out := stdout.String(); !strings.HasPrefix(out, "example.com/app/pkg.test\n") || strings.Contains(out, "example.com/app\n") {
			t.Errorf("unexpected output:\n%s", out)
		}
	})

	t.Run("no report", func(t *testing.T) {
		c := &CLI{stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
		err := c.runAnalysisCmd(context.Background(), []string{t.TempDir()})
		if err == nil || !strings.Contains(err.Error(), "no analysis report found") {
			t.Fatalf("expected a no-report error, got %v", err)
		}
	})

	t.Run("invalid threshold", func(t *testing.T) {
		c := &CLI{stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
		if err := c.runAnalysisCmd(context.Background(), []string{"-min-share", "101", root}); err == nil {
			t.Fatal("expected an error")
		}
	})
}
