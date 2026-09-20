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
)

func packageAttrs(loaded *loadedProgram, pkgPath string) []telemetry.Attr {
	return []telemetry.Attr{
		telemetry.String("tobari.package", pkgPath),
		telemetry.String("tobari.module", loaded.modules[pkgPath]),
	}
}

// buildSSA builds the function bodies of every package skip does not reject.
// A skipped package stays bodyless, so RTA cannot trace calls through it.
//
// Packages are built concurrently, as ssa.Program.Build does, but one by one
// rather than through that method so that each build can be timed. The time
// reported for a package is wall-clock time under that concurrency, and it
// includes the generic instantiations the package is the first to demand, so
// it ranks packages against each other rather than measuring them in
// isolation.
func buildSSA(ctx context.Context, prog *ssa.Program, loaded *loadedProgram, skip func(*ssa.Package) bool) {
	ctx, span := telemetry.Start(ctx, "tobari.analysis.ssa_build")
	defer span.End()

	var targets []*ssa.Package
	skipped := 0
	for _, ssaPkg := range prog.AllPackages() {
		if ssaPkg.Pkg != nil && skip(ssaPkg) {
			skipped++
			telemetry.RecordCount(ctx, metricSkipped, 1, packageAttrs(loaded, ssaPkg.Pkg.Path())...)
			continue
		}
		targets = append(targets, ssaPkg)
	}
	sort.Slice(targets, func(i, j int) bool {
		return ssaPkgPath(targets[i]) < ssaPkgPath(targets[j])
	})
	span.SetAttrs(
		telemetry.Int("tobari.analysis.built_packages", len(targets)),
		telemetry.Int("tobari.analysis.skipped_packages", skipped),
	)

	queue := make(chan *ssa.Package)
	var wg sync.WaitGroup
	for range min(runtime.GOMAXPROCS(0), len(targets)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ssaPkg := range queue {
				attrs := packageAttrs(loaded, ssaPkgPath(ssaPkg))
				_, pkgSpan := telemetry.Start(ctx, "tobari.analysis.ssa_build.package", attrs...)
				start := time.Now()
				ssaPkg.Build()
				telemetry.RecordSeconds(ctx, metricSSABuildSeconds, time.Since(start), attrs...)
				pkgSpan.End()
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
func recordReachability(ctx context.Context, loaded *loadedProgram, graph *callgraph.Graph) {
	if !telemetry.Enabled(ctx) {
		return
	}
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
	pkgPaths := make([]string, 0, len(sizes))
	for pkgPath := range sizes {
		pkgPaths = append(pkgPaths, pkgPath)
	}
	sort.Strings(pkgPaths)
	for _, pkgPath := range pkgPaths {
		attrs := packageAttrs(loaded, pkgPath)
		telemetry.RecordCount(ctx, metricReachableFunctions, sizes[pkgPath].functions, attrs...)
		telemetry.RecordCount(ctx, metricCallEdges, sizes[pkgPath].edges, attrs...)
	}
}
