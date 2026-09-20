package cover

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"time"

	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"

	"github.com/goccy/tobari/internal/telemetry"
	"github.com/goccy/tobari/internal/utils"
	"github.com/goccy/tobari/internal/version"
)

// Learned skips: leaving dependencies out of the analysis without being told.
//
// --exclude-analysis makes the analysis faster by not building the function
// bodies of packages the user asserts to be irrelevant. tobari can find such
// packages itself, with evidence instead of an assertion: after an analysis it
// repeats the analysis without the packages no call path between coverage
// targets ran through, and if the dependencies come out identical, leaving
// those packages out provably does not change the result for this program. It
// records that, and the next analysis of the same target skips them.
//
// The record must not outlive what it was learned from. It is dropped as a
// whole when the dependency versions, the coverage targets, the user's own
// exclusions, tobari or Go change. Against changes of the code under
// development it is kept per package: see boundaries. When the way that code
// uses an outside package B changes, what was learned about B and about every
// package B imports, directly or not, is dropped and those packages are
// analyzed again — and nothing else is.
//
// What this cannot see is a change that reaches a skipped package without
// touching its boundary or the boundary of anything importing it, e.g. a
// callback registered with package C that skipped package B, which does not
// import C, is later made to run. TOBARI_ANALYSIS=verify exists to check the
// record against a full analysis.

// EnvAnalysis selects how the analysis treats the learned skips; see the
// analysisMode constants.
const EnvAnalysis = "TOBARI_ANALYSIS"

type analysisMode string

const (
	// analysisModeAuto uses and updates the record.
	analysisModeAuto analysisMode = ""
	// analysisModeFull neither reads nor writes the record.
	analysisModeFull analysisMode = "full"
	// analysisModeVerify analyzes in full, checks that the recorded skips
	// would have produced the same result, and fails the build if not.
	analysisModeVerify analysisMode = "verify"
)

func analysisModeFromEnv() (analysisMode, error) {
	switch mode := analysisMode(os.Getenv(EnvAnalysis)); mode {
	case analysisModeAuto, analysisModeFull, analysisModeVerify:
		return mode, nil
	default:
		return "", fmt.Errorf("%s must be %q or %q, got %q", EnvAnalysis, analysisModeFull, analysisModeVerify, mode)
	}
}

// learnMinShare is the share of the analysis time the candidates must add up
// to for the verification analysis to be worth running, and learnMinBudget is
// the time verification may always take, however short the analysis was. Both
// only trade one cost for another and have no bearing on the result.
const (
	learnMinShare  = 0.1
	learnMinBudget = 30 * time.Second
)

// skipRecord is what was learned about one target.
type skipRecord struct {
	Target string `json:"target"`
	// Context fingerprints everything the record depends on as a whole.
	Context string `json:"context"`
	// Boundaries are the boundaries the verdicts were learned under.
	Boundaries boundaries `json:"boundaries"`
	// Verdicts maps package import paths to what was learned about them.
	Verdicts map[string]SkipVerdict `json:"verdicts"`
}

func skipRecordPath(reportDir, target string) string {
	return filepath.Join(reportDir, utils.AnalysisLearnedDirName, filepath.Base(analysisReportPath(reportDir, target)))
}

// readSkipRecord returns the record of target, or nil when there is none that
// can be used: no file, a file that does not decode (it may stem from another
// tobari version), or one written for another target or context.
func readSkipRecord(path, target, recordContext string) *skipRecord {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var record skipRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return nil
	}
	if record.Target != target || record.Context != recordContext {
		return nil
	}
	return &record
}

func writeSkipRecord(path string, record *skipRecord) error {
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode learned skips: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("failed to create %s: %w", filepath.Dir(path), err)
	}
	if err := utils.AtomicWriteFile(path, append(data, '\n')); err != nil {
		return fmt.Errorf("failed to write learned skips: %w", err)
	}
	return nil
}

// recordContext fingerprints what a skip record depends on as a whole: the
// tools, the coverage targets, the user's exclusions, and the identity of
// every immutable package — its module version and its file names, which vary
// with build tags and the target platform.
func recordContext(loaded *loadedProgram, excludeAnalysis []string) (string, error) {
	ver, err := version.Get()
	if err != nil {
		return "", fmt.Errorf("failed to get tobari version: %w", err)
	}
	goVersion, err := utils.GoVersion()
	if err != nil {
		return "", err
	}
	type immutablePackage struct {
		Package string   `json:"package"`
		Version string   `json:"version"`
		Files   []string `json:"files"`
	}
	input := struct {
		Tobari          string             `json:"tobari"`
		Go              string             `json:"go"`
		CoverPackages   []string           `json:"coverPackages"`
		ExcludeAnalysis []string           `json:"excludeAnalysis"`
		Packages        []immutablePackage `json:"packages"`
	}{
		Tobari:          ver.ID(),
		Go:              goVersion,
		CoverPackages:   sortedKeys(loaded.coverPkgSet),
		ExcludeAnalysis: excludeAnalysis,
	}
	for _, pkgPath := range sortedKeys(loaded.listed) {
		listed := loaded.listed[pkgPath]
		if pkgVersion, ok := listed.immutableVersion(); ok {
			input.Packages = append(input.Packages, immutablePackage{Package: pkgPath, Version: pkgVersion, Files: listed.GoFiles})
		}
	}
	data, err := json.Marshal(input)
	if err != nil {
		return "", fmt.Errorf("failed to encode the record context: %w", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// importClosures answers which packages a package imports, directly or not.
type importClosures struct {
	byID map[string]*packages.Package
}

func newImportClosures(pkgs []*packages.Package) *importClosures {
	c := &importClosures{byID: make(map[string]*packages.Package)}
	packages.Visit(pkgs, nil, func(pkg *packages.Package) { c.byID[pkg.ID] = pkg })
	return c
}

// addClosure adds pkgPath and everything it imports to set.
func (c *importClosures) addClosure(set map[string]struct{}, pkgPath string) {
	pending := []string{pkgPath}
	for len(pending) > 0 {
		id := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if _, ok := set[id]; ok {
			continue
		}
		set[id] = struct{}{}
		if pkg := c.byID[id]; pkg != nil {
			for _, imported := range pkg.Imports {
				pending = append(pending, imported.ID)
			}
		}
	}
}

// validVerdicts returns the verdicts of record that still hold under the
// current boundaries: those of packages outside the import closure of every
// package whose boundary changed.
func validVerdicts(record *skipRecord, current boundaries, closures *importClosures) map[string]SkipVerdict {
	valid := make(map[string]SkipVerdict)
	if record == nil {
		return valid
	}
	invalidated := make(map[string]struct{})
	for _, pkgPath := range current.changedFrom(record.Boundaries) {
		closures.addClosure(invalidated, pkgPath)
	}
	for pkgPath, verdict := range record.Verdicts {
		if _, ok := invalidated[pkgPath]; !ok {
			valid[pkgPath] = verdict
		}
	}
	return valid
}

// analyzeLearning runs the analysis of target the way TOBARI_ANALYSIS asks for.
func analyzeLearning(ctx context.Context, rec *analysisRecorder, loaded *loadedProgram, target string, excludeAnalysis []string) (map[string][]string, error) {
	userSkip := func(ssaPkg *ssa.Package) bool {
		return !isMainPkg(ssaPkg) && isExcludedFromAnalysis(ssaPkg.Pkg.Path(), excludeAnalysis, loaded.coverPkgSet)
	}
	mode, err := analysisModeFromEnv()
	if err != nil {
		return nil, err
	}
	if mode == analysisModeFull || target == "" {
		suppDeps, _ := analyze(ctx, rec, loaded, skipPolicy{fixed: userSkip})
		return suppDeps, nil
	}

	recordCtx, err := recordContext(loaded, excludeAnalysis)
	if err != nil {
		return nil, err
	}
	l := &learner{
		loaded:   loaded,
		userSkip: userSkip,
		closures: newImportClosures(loaded.pkgs),
		path:     skipRecordPath(AnalysisReportDir(loaded.dir), target),
		target:   target,
		context:  recordCtx,
	}
	l.record = readSkipRecord(l.path, target, recordCtx)

	if mode == analysisModeVerify {
		return l.verifyRecord(ctx, rec)
	}
	return l.analyze(ctx, rec), nil
}

type learner struct {
	loaded   *loadedProgram
	userSkip func(*ssa.Package) bool
	closures *importClosures
	path     string
	target   string
	context  string
	record   *skipRecord
}

func skipSetOf(verdicts map[string]SkipVerdict) map[string]struct{} {
	set := make(map[string]struct{})
	for pkgPath, verdict := range verdicts {
		if verdict == VerdictSkip {
			set[pkgPath] = struct{}{}
		}
	}
	return set
}

// analyze runs the analysis with the learned skips that still hold, then
// tries to learn more, and stores the outcome.
func (l *learner) analyze(ctx context.Context, rec *analysisRecorder) map[string][]string {
	var verdicts map[string]SkipVerdict
	start := time.Now()
	suppDeps, current := analyze(ctx, rec, l.loaded, skipPolicy{
		fixed: l.userSkip,
		learned: func(current boundaries) map[string]struct{} {
			verdicts = validVerdicts(l.record, current, l.closures)
			return skipSetOf(verdicts)
		},
	})
	if suppDeps == nil {
		// Nothing was analyzed (no entry point), so nothing was learned.
		return nil
	}
	l.learn(ctx, rec.report(l.target), suppDeps, verdicts, time.Since(start))
	for pkgPath, verdict := range verdicts {
		rec.update(l.loaded, pkgPath, func(pkg *AnalysisPackage) { pkg.Verdict = verdict })
	}

	// The record is a by-product; see the analysis report for why a failure
	// to write it does not fail the build.
	record := &skipRecord{Target: l.target, Context: l.context, Boundaries: current, Verdicts: verdicts}
	if err := writeSkipRecord(l.path, record); err != nil {
		fmt.Fprintf(os.Stderr, "tobari: %v\n", err)
	}
	return suppDeps
}

// candidates returns the packages worth trying to leave out, most expensive
// first: analyzed in this build, on no call path between coverage targets,
// pinned by a version, and without a verdict. It returns nil when they are too
// cheap to bother.
//
// Being on no call path is what makes a package worth trying, not what makes
// leaving it out safe — a package can matter by carrying a value, or by making
// a function reachable at all — so nothing is ruled out here on the grounds
// that it probably matters. The verification decides.
func (l *learner) candidates(report *AnalysisReport, verdicts map[string]SkipVerdict) []string {
	seconds, attributable := report.EstimatedSeconds()
	var (
		pkgPaths []string
		total    float64
	)
	for _, pkg := range report.Packages {
		if pkg.Required || pkg.OnPath || pkg.Skipped {
			continue
		}
		if _, ok := verdicts[pkg.Package]; ok {
			continue
		}
		if listed := l.loaded.listed[pkg.Package]; listed == nil || !listed.immutable() {
			continue
		}
		pkgPaths = append(pkgPaths, pkg.Package)
		total += seconds[pkg.Package]
	}
	if attributable <= 0 || total/attributable < learnMinShare {
		return nil
	}
	sort.SliceStable(pkgPaths, func(i, j int) bool { return seconds[pkgPaths[i]] > seconds[pkgPaths[j]] })
	return pkgPaths
}

// learn extends verdicts with what can be verified about the candidates.
//
// All candidates are tried at once first, which costs one reduced analysis and
// settles the matter when none of them matters. A set that changes the result
// is halved and each half tried on its own, down to the single packages that
// cannot be left out. Every set is tried on top of everything accepted before
// it, so what ends up accepted has been verified as a whole, not only piece by
// piece. The search stops once it has taken as long as the analysis itself
// (but at least learnMinBudget); packages it did not get to keep no verdict
// and are candidates again in a later build.
func (l *learner) learn(ctx context.Context, report *AnalysisReport, baseline map[string][]string, verdicts map[string]SkipVerdict, analysisTime time.Duration) {
	candidates := l.candidates(report, verdicts)
	if len(candidates) == 0 {
		return
	}
	ctx, span := telemetry.Start(ctx, "tobari.analysis.learn", telemetry.Int("tobari.analysis.candidate_packages", len(candidates)))
	defer span.End()

	var (
		start    = time.Now()
		budget   = max(analysisTime, learnMinBudget)
		accepted = skipSetOf(verdicts)
		search   func(pkgPaths []string)
	)
	search = func(pkgPaths []string) {
		if time.Since(start) > budget {
			return
		}
		skip := make(map[string]struct{}, len(accepted)+len(pkgPaths))
		for pkgPath := range accepted {
			skip[pkgPath] = struct{}{}
		}
		for _, pkgPath := range pkgPaths {
			skip[pkgPath] = struct{}{}
		}
		if l.sameResult(ctx, baseline, skip) {
			accepted = skip
			for _, pkgPath := range pkgPaths {
				verdicts[pkgPath] = VerdictSkip
			}
			return
		}
		if len(pkgPaths) == 1 {
			verdicts[pkgPaths[0]] = VerdictKeep
			return
		}
		half := len(pkgPaths) / 2
		search(pkgPaths[:half])
		search(pkgPaths[half:])
	}
	search(candidates)
}

// sameResult reports whether the analysis still produces baseline when the
// packages in skip are left out in addition to the user's exclusions.
func (l *learner) sameResult(ctx context.Context, baseline map[string][]string, skip map[string]struct{}) bool {
	ctx, span := telemetry.Start(telemetry.MuteMetrics(ctx), "tobari.analysis.verify", telemetry.Int("tobari.analysis.skipped_packages", len(skip)))
	defer span.End()

	got, _ := analyze(ctx, newAnalysisRecorder(), l.loaded, skipPolicy{
		fixed: func(ssaPkg *ssa.Package) bool {
			if l.userSkip(ssaPkg) {
				return true
			}
			_, ok := skip[ssaPkg.Pkg.Path()]
			return ok
		},
	})
	same := reflect.DeepEqual(got, baseline)
	span.SetAttrs(telemetry.Bool("tobari.analysis.same_result", same))
	return same
}

// verifyRecord analyzes in full and checks the recorded skips against it.
func (l *learner) verifyRecord(ctx context.Context, rec *analysisRecorder) (map[string][]string, error) {
	var skip map[string]struct{}
	suppDeps, _ := analyze(ctx, rec, l.loaded, skipPolicy{
		fixed: l.userSkip,
		learned: func(current boundaries) map[string]struct{} {
			// Only the skips a normal build would apply are under test. They
			// are computed here, where the boundaries are known, and not
			// applied: this analysis is the full one.
			skip = skipSetOf(validVerdicts(l.record, current, l.closures))
			return nil
		},
	})
	if suppDeps == nil || len(skip) == 0 {
		return suppDeps, nil
	}
	if l.sameResult(ctx, suppDeps, skip) {
		return suppDeps, nil
	}
	if err := os.Remove(l.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, errors.Join(l.changedResultError(), err)
	}
	return nil, l.changedResultError()
}

func (l *learner) changedResultError() error {
	return fmt.Errorf("the learned skips of %s change the result of the dependency analysis; they have been discarded (%s)", l.target, l.path)
}
