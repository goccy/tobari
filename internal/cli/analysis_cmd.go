package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/goccy/tobari/internal/cover"
	"github.com/goccy/tobari/internal/utils"
)

const analysisUsage = "Usage: tobari analysis [-json] [-min-share percent] [dir...]"

// analysisSummary is the per-module view of one cover.AnalysisReport.
type analysisSummary struct {
	Target string `json:"target"`
	// Seconds is the wall-clock time of the whole analysis.
	Seconds float64               `json:"seconds"`
	Stages  []cover.AnalysisStage `json:"stages"`
	// Modules is sorted by estimated cost, most expensive first.
	Modules []*moduleSummary `json:"modules"`
	// Candidates lists the modules worth trying in --exclude-analysis, most
	// expensive first.
	Candidates []string `json:"candidates"`
	// Verified lists the candidates the build verified: repeating the
	// analysis without them gave the same result.
	Verified []string `json:"verified"`
}

// moduleSummary sums up the packages of one module.
type moduleSummary struct {
	Module             string  `json:"module"`
	Packages           int     `json:"packages"`
	SSABuildSeconds    float64 `json:"ssaBuildSeconds"`
	ReachableFunctions int     `json:"reachableFunctions"`
	CallEdges          int     `json:"callEdges"`
	// EstimatedSeconds is the module's share of the analysis time. Only the
	// SSA build is measured per package; the call graph stages are
	// whole-program computations, so their time is split by each module's
	// share of the call edges. It ranks modules; it is not a measurement.
	EstimatedSeconds float64 `json:"estimatedSeconds"`
	// Share is EstimatedSeconds as a fraction of the attributable time.
	Share float64 `json:"share"`
	// Role is what the module's packages were to the analysis; see the
	// moduleRole constants.
	Role moduleRole `json:"role"`
}

type moduleRole string

const (
	// roleRequired: the module holds a coverage target or the main/test
	// package, which are never excluded.
	roleRequired moduleRole = "required"
	// roleOnPath: a call path between coverage targets runs through the
	// module, so excluding it would lose dependencies.
	roleOnPath moduleRole = "on-path"
	// roleSkipped: every package of the module was already left out by
	// --exclude-analysis.
	roleSkipped moduleRole = "skipped"
	// roleLearned: every package of the module was left out, at least one of
	// them because an earlier build verified that it does not change the
	// result.
	roleLearned moduleRole = "skipped (learned)"
	// roleOffPath: analyzed, and no call path between coverage targets runs
	// through it.
	roleOffPath moduleRole = "off-path"
	// roleVerified: off-path, and repeating the analysis without the module
	// gave the same result.
	roleVerified moduleRole = "off-path (verified)"
	// roleNeeded: off-path, but repeating the analysis without the module
	// changed the result: it carries values the analysis has to follow, say,
	// or makes coverage-target functions reachable in the first place.
	roleNeeded moduleRole = "needed"
)

// unknownModule groups the packages go list did not report, such as the
// synthesized test main.
const unknownModule = "(unknown)"

// stdModule is the module name analysis reports use for the standard library.
// Its packages have no common path prefix, so it cannot be excluded as one.
const stdModule = "std"

// summarizeAnalysis folds a report into modules. minShare is the fraction of
// the attributable analysis time a module must reach to be listed as a
// candidate.
func summarizeAnalysis(report *cover.AnalysisReport, minShare float64) *analysisSummary {
	summary := &analysisSummary{Target: report.Target, Stages: report.Stages}

	for _, stage := range report.Stages {
		summary.Seconds += stage.Seconds
	}
	estimated, attributable := report.EstimatedSeconds()

	type moduleState struct {
		summary                            *moduleSummary
		required, onPath, skipped, learned bool
		// needed is set when any package has the keep verdict, verified while
		// every analyzed package has the skip verdict.
		needed, verified bool
	}
	modules := make(map[string]*moduleState)
	for _, pkg := range report.Packages {
		name := pkg.Module
		if name == "" {
			name = unknownModule
		}
		m := modules[name]
		if m == nil {
			m = &moduleState{summary: &moduleSummary{Module: name}, skipped: true, verified: true}
			modules[name] = m
		}
		m.summary.Packages++
		m.summary.SSABuildSeconds += pkg.SSABuildSeconds
		m.summary.ReachableFunctions += pkg.ReachableFunctions
		m.summary.CallEdges += pkg.CallEdges
		m.required = m.required || pkg.Required
		m.onPath = m.onPath || pkg.OnPath
		m.skipped = m.skipped && pkg.Skipped
		m.learned = m.learned || pkg.Learned
		m.needed = m.needed || pkg.Verdict == cover.VerdictKeep
		m.verified = m.verified && (pkg.Skipped || pkg.Verdict == cover.VerdictSkip)
		m.summary.EstimatedSeconds += estimated[pkg.Package]
	}

	for _, m := range modules {
		switch {
		case m.required:
			m.summary.Role = roleRequired
		case m.onPath:
			m.summary.Role = roleOnPath
		case m.skipped && m.learned:
			m.summary.Role = roleLearned
		case m.skipped:
			m.summary.Role = roleSkipped
		case m.needed:
			m.summary.Role = roleNeeded
		case m.verified:
			m.summary.Role = roleVerified
		default:
			m.summary.Role = roleOffPath
		}
		if attributable > 0 {
			m.summary.Share = m.summary.EstimatedSeconds / attributable
		}
		summary.Modules = append(summary.Modules, m.summary)
	}
	sort.Slice(summary.Modules, func(i, j int) bool {
		a, b := summary.Modules[i], summary.Modules[j]
		if a.EstimatedSeconds != b.EstimatedSeconds {
			return a.EstimatedSeconds > b.EstimatedSeconds
		}
		return a.Module < b.Module
	})

	summary.Candidates = []string{}
	summary.Verified = []string{}
	for _, m := range summary.Modules {
		if (m.Role != roleOffPath && m.Role != roleVerified) || m.Module == stdModule || m.Module == unknownModule {
			continue
		}
		if m.Share < minShare {
			continue
		}
		summary.Candidates = append(summary.Candidates, m.Module)
		if m.Role == roleVerified {
			summary.Verified = append(summary.Verified, m.Module)
		}
	}
	return summary
}

// findAnalysisReportDirs returns the analysis report directories at or below
// root. Hidden directories are not searched.
func findAnalysisReportDirs(root string) ([]string, error) {
	var dirs []string
	suffix := filepath.Join(utils.CoverOutputDirName, utils.AnalysisReportDirName)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if path != root && strings.HasPrefix(d.Name(), ".") {
			return filepath.SkipDir
		}
		if d.Name() == utils.AnalysisReportDirName && filepath.Base(filepath.Dir(path)) == utils.CoverOutputDirName {
			dirs = append(dirs, path)
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to search %s for %s: %w", root, suffix, err)
	}
	return dirs, nil
}

func (c *CLI) runAnalysisCmd(_ context.Context, args []string) error {
	flagSet := flag.NewFlagSet("analysis", flag.ContinueOnError)
	flagSet.SetOutput(c.stderr)
	asJSON := flagSet.Bool("json", false, "print the summary as JSON")
	minSharePercent := flagSet.Float64("min-share", 1, "minimum share of the analysis time, in percent, for a module to be listed as a candidate")
	if err := flagSet.Parse(args); err != nil {
		return err
	}
	if *minSharePercent < 0 || *minSharePercent > 100 {
		return fmt.Errorf("-min-share must be between 0 and 100\n%s", analysisUsage)
	}

	roots := flagSet.Args()
	if len(roots) == 0 {
		roots = []string{"."}
		if dir := os.Getenv(utils.EnvCoverDir); dir != "" {
			roots = []string{dir}
		}
	}
	var summaries []*analysisSummary
	for _, root := range roots {
		dirs, err := findAnalysisReportDirs(root)
		if err != nil {
			return err
		}
		for _, dir := range dirs {
			reports, err := cover.ReadAnalysisReports(dir)
			if err != nil {
				return err
			}
			for _, report := range reports {
				summaries = append(summaries, summarizeAnalysis(report, *minSharePercent/100))
			}
		}
	}
	if len(summaries) == 0 {
		return fmt.Errorf("no analysis report found under %s\nBuild with tobari first; a build served from Go's build cache runs no analysis", strings.Join(roots, ", "))
	}
	sort.SliceStable(summaries, func(i, j int) bool { return summaries[i].Target < summaries[j].Target })

	if *asJSON {
		enc := json.NewEncoder(c.stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(summaries)
	}
	for i, summary := range summaries {
		if i != 0 {
			if _, err := fmt.Fprintln(c.stdout); err != nil {
				return err
			}
		}
		if err := c.printAnalysisSummary(summary); err != nil {
			return err
		}
	}
	return nil
}

func (c *CLI) printAnalysisSummary(summary *analysisSummary) error {
	stages := make([]string, 0, len(summary.Stages))
	for _, stage := range summary.Stages {
		stages = append(stages, fmt.Sprintf("%s %.2fs", stage.Name, stage.Seconds))
	}
	if _, err := fmt.Fprintf(c.stdout, "%s\n  analysis %.2fs (%s)\n\n", summary.Target, summary.Seconds, strings.Join(stages, ", ")); err != nil {
		return err
	}

	w := tabwriter.NewWriter(c.stdout, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(w, "  MODULE\tPACKAGES\tEST. TIME\tSHARE\tFUNCTIONS\tCALL EDGES\tROLE"); err != nil {
		return err
	}
	for _, m := range summary.Modules {
		if _, err := fmt.Fprintf(w, "  %s\t%d\t%.2fs\t%.1f%%\t%d\t%d\t%s\n",
			m.Module, m.Packages, m.EstimatedSeconds, m.Share*100, m.ReachableFunctions, m.CallEdges, m.Role); err != nil {
			return err
		}
	}
	if err := w.Flush(); err != nil {
		return err
	}

	if len(summary.Candidates) == 0 {
		_, err := fmt.Fprintln(c.stdout, "\n  No module is both expensive and off every call path between coverage targets.")
		return err
	}
	if len(summary.Verified) == len(summary.Candidates) {
		_, err := fmt.Fprintf(c.stdout, `
  Repeating the analysis without these modules gave the same result, and tobari
  leaves them out by itself from the next build on. To exclude them for good:

    -exclude-analysis=%s
`, strings.Join(summary.Candidates, ","))
		return err
	}
	_, err := fmt.Fprintf(c.stdout, `
  No call path between coverage targets ran through these modules in this
  build. Exclude only those you never hand a callback or an interface value
  implemented by your code:

    -exclude-analysis=%s
`, strings.Join(summary.Candidates, ","))
	return err
}
