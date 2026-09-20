package cover

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/goccy/tobari/internal/telemetry"
	"github.com/goccy/tobari/internal/utils"
)

// AnalysisReport is what one whole-program dependency analysis cost and what
// each package meant to its result. The cover tool writes one per analyzed
// target (a main package or a test binary) and `tobari analysis` reads them to
// show which dependencies are worth passing to --exclude-analysis.
type AnalysisReport struct {
	// Target is the import path of the analyzed main package or test binary.
	Target string `json:"target"`
	// Stages holds the wall-clock seconds of each analysis stage.
	Stages []AnalysisStage `json:"stages"`
	// Packages holds every package of the program, sorted by import path.
	Packages []*AnalysisPackage `json:"packages"`
}

// AnalysisStage is the duration of one stage of the analysis.
type AnalysisStage struct {
	Name    string  `json:"name"`
	Seconds float64 `json:"seconds"`
}

// AnalysisPackage is one package's part in an analysis.
type AnalysisPackage struct {
	Package string `json:"package"`
	// Module is the module providing the package, "std" for the standard
	// library, or "" when go list did not report the package.
	Module string `json:"module,omitempty"`
	// SSABuildSeconds is the wall-clock time of the package's SSA build; see
	// buildSSA for how to read it.
	SSABuildSeconds float64 `json:"ssaBuildSeconds"`
	// ReachableFunctions and CallEdges are the package's share of the RTA
	// call graph: its functions in the graph and the edges leaving them.
	ReachableFunctions int `json:"reachableFunctions"`
	CallEdges          int `json:"callEdges"`
	// Required is set for coverage targets and main/test packages, which the
	// analysis exists to measure and therefore never skips.
	Required bool `json:"required,omitempty"`
	// Skipped is set when the package's function bodies were left out of the
	// analysis.
	Skipped bool `json:"skipped,omitempty"`
	// Learned is set on a skipped package that was left out because an
	// earlier build verified that doing so does not change the result, rather
	// than because --exclude-analysis names it.
	Learned bool `json:"learned,omitempty"`
	// Verdict is what repeating the analysis without the package showed:
	// VerdictSkip when the result stayed the same, VerdictKeep when it
	// changed. It is empty for a package that was never put to the test.
	Verdict SkipVerdict `json:"verdict,omitempty"`
	// OnPath is set when a call path from one coverage-target function to
	// another runs through a function of this package, which makes the
	// package part of the computed dependencies: leaving it out would lose
	// them. A package that is not on any path contributed no such path in
	// this build; that is evidence that skipping it is safe, not proof,
	// because a package can also matter by carrying a value from one place
	// to another without being called in between.
	OnPath bool `json:"onPath,omitempty"`
}

// SkipVerdict is the outcome of verifying that a package can be left out of
// the analysis.
type SkipVerdict string

const (
	// VerdictSkip: leaving the package out was verified not to change the
	// result.
	VerdictSkip SkipVerdict = "skip"
	// VerdictKeep: leaving the package out changed the result, so it is not
	// tried again while the record holds.
	VerdictKeep SkipVerdict = "keep"
)

// Stages whose time scales with the call graph rather than with source size.
var callGraphStages = map[string]struct{}{
	"rta":                {},
	"vta":                {},
	"followable_callees": {},
	"frontiers":          {},
	"dependencies":       {},
}

const ssaBuildStage = "ssa_build"

// EstimatedSeconds splits the analysis time that depends on the packages
// analyzed among them, and also returns that time as a whole.
//
// Only the SSA build is measured per package, and under concurrency, so the
// duration of the SSA build stage is split by those measurements. The call
// graph stages are whole-program computations; their time is split by each
// package's share of the call edges. The result ranks packages. It is not a
// measurement.
func (r *AnalysisReport) EstimatedSeconds() (map[string]float64, float64) {
	var ssaStageSeconds, callGraphSeconds float64
	for _, stage := range r.Stages {
		if stage.Name == ssaBuildStage {
			ssaStageSeconds += stage.Seconds
		}
		if _, ok := callGraphStages[stage.Name]; ok {
			callGraphSeconds += stage.Seconds
		}
	}
	var totalSSASeconds float64
	var totalEdges int
	for _, pkg := range r.Packages {
		totalSSASeconds += pkg.SSABuildSeconds
		totalEdges += pkg.CallEdges
	}
	seconds := make(map[string]float64, len(r.Packages))
	for _, pkg := range r.Packages {
		if totalSSASeconds > 0 {
			seconds[pkg.Package] += ssaStageSeconds * pkg.SSABuildSeconds / totalSSASeconds
		}
		if totalEdges > 0 {
			seconds[pkg.Package] += callGraphSeconds * float64(pkg.CallEdges) / float64(totalEdges)
		}
	}
	return seconds, ssaStageSeconds + callGraphSeconds
}

// analysisRecorder accumulates an AnalysisReport while the analysis runs.
type analysisRecorder struct {
	mu       sync.Mutex
	stages   []AnalysisStage
	packages map[string]*AnalysisPackage
}

func newAnalysisRecorder() *analysisRecorder {
	return &analysisRecorder{packages: make(map[string]*AnalysisPackage)}
}

// stage times one stage of the analysis, as a telemetry span and as an entry
// of the report. The returned function ends the stage.
func (r *analysisRecorder) stage(ctx context.Context, name string, attrs ...telemetry.Attr) (context.Context, func(...telemetry.Attr)) {
	ctx, span := telemetry.Start(ctx, "tobari.analysis."+name, attrs...)
	start := time.Now()
	return ctx, func(attrs ...telemetry.Attr) {
		span.SetAttrs(attrs...)
		span.End()
		r.mu.Lock()
		defer r.mu.Unlock()
		r.stages = append(r.stages, AnalysisStage{Name: name, Seconds: time.Since(start).Seconds()})
	}
}

// update applies fn to the entry of pkgPath, creating it on first use.
func (r *analysisRecorder) update(loaded *loadedProgram, pkgPath string, fn func(*AnalysisPackage)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	pkg := r.packages[pkgPath]
	if pkg == nil {
		pkg = &AnalysisPackage{Package: pkgPath, Module: loaded.modules[pkgPath]}
		r.packages[pkgPath] = pkg
	}
	fn(pkg)
}

func (r *analysisRecorder) report(target string) *AnalysisReport {
	r.mu.Lock()
	defer r.mu.Unlock()
	report := &AnalysisReport{Target: target, Stages: r.stages}
	for _, pkg := range r.packages {
		report.Packages = append(report.Packages, pkg)
	}
	sort.Slice(report.Packages, func(i, j int) bool {
		return report.Packages[i].Package < report.Packages[j].Package
	})
	return report
}

// AnalysisReportDir returns the directory the analysis reports of the package
// in pkgDir live in: next to the coverage results its tests write, which go to
// the "tobari" directory under TOBARI_COVERDIR or, by default, under the
// package directory the test binary runs in. A relative TOBARI_COVERDIR is
// resolved the same way, against the package directory.
func AnalysisReportDir(pkgDir string) string {
	baseDir := pkgDir
	if dir := os.Getenv(utils.EnvCoverDir); dir != "" {
		baseDir = dir
		if !filepath.IsAbs(dir) {
			baseDir = filepath.Join(pkgDir, dir)
		}
	}
	return filepath.Join(baseDir, utils.CoverOutputDirName, utils.AnalysisReportDirName)
}

// analysisReportPath returns the file a target's report is stored in. Import
// paths contain slashes, so the target is path-escaped into a single file
// name that maps back to it.
func analysisReportPath(dir, target string) string {
	return filepath.Join(dir, url.PathEscape(target)+".json")
}

// writeAnalysisReport stores the report of one target, replacing the one of
// the target's previous build. Several analyses of one `go test ./...` write
// here concurrently, each to the file of its own target.
func writeAnalysisReport(dir string, report *AnalysisReport) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode analysis report: %w", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create %s: %w", dir, err)
	}
	if err := utils.AtomicWriteFile(analysisReportPath(dir, report.Target), append(data, '\n')); err != nil {
		return fmt.Errorf("failed to write analysis report: %w", err)
	}
	return nil
}

// ReadAnalysisReports reads every report in dir, sorted by target.
func ReadAnalysisReports(dir string) ([]*AnalysisReport, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	reports := make([]*AnalysisReport, 0, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var report AnalysisReport
		if err := json.Unmarshal(data, &report); err != nil {
			return nil, fmt.Errorf("failed to decode %s: %w", path, err)
		}
		reports = append(reports, &report)
	}
	sort.Slice(reports, func(i, j int) bool { return reports[i].Target < reports[j].Target })
	return reports, nil
}
