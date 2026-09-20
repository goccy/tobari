package cover

import (
	"context"
	"runtime"
	"sort"
	"sync"
	"time"

	"golang.org/x/tools/go/callgraph"
	"golang.org/x/tools/go/ssa"

	"github.com/goccy/tobari/internal/telemetry"
)

// Metric names reported per analyzed package. Every point carries the
// package's import path and the module providing it, so the cost of a
// dependency can be read per package or summed per module — and a module path
// is exactly what --exclude-analysis takes.
const (
	metricSSABuildSeconds    = "tobari.analysis.package.ssa_build.duration"
	metricReachableFunctions = "tobari.analysis.package.reachable_functions"
	metricCallEdges          = "tobari.analysis.package.call_edges"
	metricSkipped            = "tobari.analysis.package.skipped"
	metricOnPath             = "tobari.analysis.package.on_path"
)

func packageAttrs(loaded *loadedProgram, pkgPath string) []telemetry.Attr {
	return []telemetry.Attr{
		telemetry.String("tobari.package", pkgPath),
		telemetry.String("tobari.module", loaded.modules[pkgPath]),
	}
}

// buildSSA builds the function bodies of every package the policy does not
// leave out. A package left out stays bodyless, so RTA cannot trace calls
// through it.
//
// The inside packages are built first: the policy may need their boundaries to
// decide about the others. The boundaries are returned if it asked for them.
//
// Packages are built concurrently, as ssa.Program.Build does, but one by one
// rather than through that method so that each build can be timed. The time
// reported for a package is wall-clock time under that concurrency, and it
// includes the generic instantiations the package is the first to demand, so
// it ranks packages against each other rather than measuring them in
// isolation.
func buildSSA(ctx context.Context, rec *analysisRecorder, prog *ssa.Program, loaded *loadedProgram, policy skipPolicy) boundaries {
	ctx, end := rec.stage(ctx, "ssa_build")

	skipped := 0
	skip := func(ssaPkg *ssa.Package, isLearned bool) {
		skipped++
		pkgPath := ssaPkg.Pkg.Path()
		rec.update(loaded, pkgPath, func(pkg *AnalysisPackage) {
			pkg.Skipped = true
			pkg.Learned = isLearned
		})
		telemetry.RecordCount(ctx, metricSkipped, 1, append(packageAttrs(loaded, pkgPath), telemetry.Bool("tobari.learned", isLearned))...)
	}

	var inside, outside []*ssa.Package
	for _, ssaPkg := range prog.AllPackages() {
		switch {
		case ssaPkg.Pkg == nil:
			inside = append(inside, ssaPkg)
		case policy.fixed(ssaPkg):
			skip(ssaPkg, false)
		case isInsidePkg(ssaPkg, loaded):
			inside = append(inside, ssaPkg)
		default:
			outside = append(outside, ssaPkg)
		}
	}
	buildSSAPackages(ctx, rec, loaded, inside)

	var (
		current boundaries
		learned map[string]struct{}
	)
	if policy.learned != nil {
		insidePkgs := make([]*ssa.Package, 0, len(inside))
		for _, ssaPkg := range inside {
			if ssaPkg.Pkg != nil {
				insidePkgs = append(insidePkgs, ssaPkg)
			}
		}
		current = computeBoundaries(prog, insidePkgs)
		learned = policy.learned(current)
	}

	var targets []*ssa.Package
	for _, ssaPkg := range outside {
		if _, isLearned := learned[ssaPkg.Pkg.Path()]; isLearned {
			skip(ssaPkg, true)
			continue
		}
		targets = append(targets, ssaPkg)
	}
	buildSSAPackages(ctx, rec, loaded, targets)

	end(
		telemetry.Int("tobari.analysis.built_packages", len(inside)+len(targets)),
		telemetry.Int("tobari.analysis.skipped_packages", skipped),
	)
	return current
}

func buildSSAPackages(ctx context.Context, rec *analysisRecorder, loaded *loadedProgram, targets []*ssa.Package) {
	sort.Slice(targets, func(i, j int) bool {
		return ssaPkgPath(targets[i]) < ssaPkgPath(targets[j])
	})

	queue := make(chan *ssa.Package)
	var wg sync.WaitGroup
	for range min(runtime.GOMAXPROCS(0), len(targets)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ssaPkg := range queue {
				pkgPath := ssaPkgPath(ssaPkg)
				attrs := packageAttrs(loaded, pkgPath)
				_, pkgSpan := telemetry.Start(ctx, "tobari.analysis.ssa_build.package", attrs...)
				start := time.Now()
				ssaPkg.Build()
				elapsed := time.Since(start)
				pkgSpan.End()
				telemetry.RecordSeconds(ctx, metricSSABuildSeconds, elapsed, attrs...)
				if ssaPkg.Pkg != nil {
					rec.update(loaded, pkgPath, func(pkg *AnalysisPackage) {
						pkg.SSABuildSeconds = elapsed.Seconds()
						pkg.Required = isRequiredPkg(ssaPkg, loaded.coverPkgSet)
					})
				}
			}
		}()
	}
	for _, ssaPkg := range targets {
		queue <- ssaPkg
	}
	close(queue)
	wg.Wait()
}

// recordReachability reports, per package, how many of its functions the RTA
// call graph holds and how many call edges leave them.
//
// RTA and VTA are whole-program fixpoints whose running time cannot be split
// by package, so these sizes are the available evidence for which packages
// the call graph stages are spending their time on: both stages do work per
// reachable function and per edge. Synthetic functions that belong to no
// package, such as wrappers and bound-method closures, are not counted.
func recordReachability(ctx context.Context, rec *analysisRecorder, loaded *loadedProgram, graph *callgraph.Graph) {
	type size struct{ functions, edges int }
	sizes := make(map[string]*size)
	for fn, node := range graph.Nodes {
		pkgPath := resolvePkgPath(fn)
		if pkgPath == "" {
			continue
		}
		s := sizes[pkgPath]
		if s == nil {
			s = &size{}
			sizes[pkgPath] = s
		}
		s.functions++
		s.edges += len(node.Out)
	}
	for _, pkgPath := range sortedKeys(sizes) {
		s := sizes[pkgPath]
		rec.update(loaded, pkgPath, func(pkg *AnalysisPackage) {
			pkg.ReachableFunctions = s.functions
			pkg.CallEdges = s.edges
		})
		attrs := packageAttrs(loaded, pkgPath)
		telemetry.RecordCount(ctx, metricReachableFunctions, s.functions, attrs...)
		telemetry.RecordCount(ctx, metricCallEdges, s.edges, attrs...)
	}
}

// recordOnPath marks the packages that a call path between two
// coverage-target functions runs through; see AnalysisPackage.OnPath.
func recordOnPath(ctx context.Context, rec *analysisRecorder, loaded *loadedProgram, graph *callgraph.Graph, followable *followableCallees, fr *frontiers) {
	onPath := onPathPackages(graph, followable, fr)
	for _, pkgPath := range sortedKeys(onPath) {
		rec.update(loaded, pkgPath, func(pkg *AnalysisPackage) { pkg.OnPath = true })
		telemetry.RecordCount(ctx, metricOnPath, 1, packageAttrs(loaded, pkgPath)...)
	}
}

// onPathPackages returns the packages owning a function that lies strictly
// between two coverage-target functions on a followable call path.
//
// Such a function is one the dependency walk expands — not a coverage target,
// not in a cut-off package — that is reached from a coverage-target function
// through expanded nodes only, and from which the walk goes on to reach a
// coverage target, i.e. whose frontier is not empty. Both halves follow the
// walk newFrontiers performs, so a package is on a path exactly when the
// dependencies computed for some coverage-target function pass through it.
func onPathPackages(graph *callgraph.Graph, followable *followableCallees, fr *frontiers) map[string]struct{} {
	isTarget := func(n *callgraph.Node) bool {
		set, ok := fr.stops[n]
		return ok && set != fr.empty
	}

	onPath := make(map[string]struct{})
	seen := make(map[*callgraph.Node]struct{})
	var pending []*callgraph.Node
	for _, n := range graph.Nodes {
		if isTarget(n) {
			pending = append(pending, followable.from(n)...)
		}
	}
	for len(pending) > 0 {
		n := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		if _, stops := fr.stops[n]; stops {
			continue
		}
		pending = append(pending, followable.from(n)...)
		if set := fr.byNode[n]; set == nil || set == fr.empty {
			continue
		}
		if pkgPath := resolvePkgPath(n.Func); pkgPath != "" {
			onPath[pkgPath] = struct{}{}
		}
	}
	return onPath
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
