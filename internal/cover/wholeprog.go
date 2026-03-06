package cover

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/callgraph"
	"golang.org/x/tools/go/callgraph/rta"
	"golang.org/x/tools/go/callgraph/vta"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

// CreateWholeProgDeps performs whole-program SSA analysis starting from the main
// package and returns dependency maps for all coverage-target packages.
// It uses RTA → VTA for maximum call graph precision.
func CreateWholeProgDeps(mainFilePath string, coverPkgs []string) (map[string][]string, error) {
	coverPkgSet := make(map[string]struct{}, len(coverPkgs))
	for _, p := range coverPkgs {
		coverPkgSet[p] = struct{}{}
	}

	dir := filepath.Dir(mainFilePath)
	cfg := &packages.Config{
		Mode: packages.LoadAllSyntax,
		Dir:  dir,
		Env:  filterGOFLAGS(os.Environ()),
	}

	pkgs, err := packages.Load(cfg, ".")
	if err != nil {
		return nil, err
	}
	var pkgErrs []error
	for _, pkg := range pkgs {
		for _, err := range pkg.Errors {
			pkgErrs = append(pkgErrs, err)
		}
	}
	if len(pkgErrs) != 0 {
		return nil, errors.Join(pkgErrs...)
	}

	prog, _ := ssautil.AllPackages(pkgs, 0)
	prog.Build()

	// Filter coverPkgSet to only packages that are actual dependencies of this
	// main package. This prevents coverpkgs recorded by other main packages
	// (sharing the same ppid directory) from inflating the analysis scope.
	allDepPaths := make(map[string]struct{})
	for _, p := range prog.AllPackages() {
		if p.Pkg != nil {
			allDepPaths[p.Pkg.Path()] = struct{}{}
		}
	}
	for pkg := range coverPkgSet {
		if _, ok := allDepPaths[pkg]; !ok {
			delete(coverPkgSet, pkg)
		}
	}

	mainPkg := prog.Package(pkgs[0].Types)
	if mainPkg == nil {
		return nil, fmt.Errorf("failed to find main SSA package")
	}

	// Collect entry points for RTA.
	var roots []*ssa.Function
	if mainFunc := mainPkg.Func("main"); mainFunc != nil {
		roots = append(roots, mainFunc)
	}
	if initFunc := mainPkg.Func("init"); initFunc != nil {
		roots = append(roots, initFunc)
	}
	// Also add init functions from coverage-target packages so their
	// init-time closures are included in the reachable set.
	for _, p := range prog.AllPackages() {
		if p.Pkg == nil {
			continue
		}
		if _, ok := coverPkgSet[p.Pkg.Path()]; !ok {
			continue
		}
		if initFunc := p.Func("init"); initFunc != nil {
			roots = append(roots, initFunc)
		}
	}
	if len(roots) == 0 {
		return nil, fmt.Errorf("no entry points found in main package")
	}

	// RTA → VTA for precise call graph.
	rtaResult := rta.Analyze(roots, true)

	// Convert rta.Reachable to map[*ssa.Function]bool for vta.CallGraph.
	reachable := make(map[*ssa.Function]bool, len(rtaResult.Reachable))
	for fn := range rtaResult.Reachable {
		reachable[fn] = true
	}

	graph := vta.CallGraph(reachable, rtaResult.CallGraph)

	// Build dependency map for coverage-target packages.
	suppDeps := make(map[string][]string)
	for _, n := range graph.Nodes {
		if n.Func == nil || n.Func.Pkg == nil || n.Func.Pkg.Pkg == nil {
			continue
		}
		if _, ok := coverPkgSet[n.Func.Pkg.Pkg.Path()]; !ok {
			continue
		}
		fnName := normalizeFuncName(n.Func)
		deps := analyzeWholeProgFuncDeps(coverPkgSet, n)
		suppDeps[fnName] = mergeDeps(suppDeps[fnName], deps)
	}

	return suppDeps, nil
}

// normalizeFuncName returns the non-instantiated function name for generic
// functions (using Origin()), or the plain name for non-generic functions.
// This ensures SSA names like "(*pkg.Result[int]).IsOk" are normalized to
// "(*pkg.Result[T]).IsOk" to match per-package metadata from go/types.
func normalizeFuncName(fn *ssa.Function) string {
	if origin := fn.Origin(); origin != nil {
		return origin.String()
	}
	return fn.String()
}

// analyzeWholeProgFuncDeps finds all functions in coverage-target packages that
// are transitively reachable from node n's callees.
func analyzeWholeProgFuncDeps(coverPkgSet map[string]struct{}, n *callgraph.Node) []string {
	depMap := make(map[string]struct{})
	seenMap := make(map[*callgraph.Node]struct{})
	for _, out := range n.Out {
		callee := out.Callee
		if _, exists := seenMap[callee]; exists {
			continue
		}
		seenMap[callee] = struct{}{}
		analyzeWholeProgFuncDepsRecursive(coverPkgSet, callee, depMap, seenMap)
	}
	deps := make([]string, 0, len(depMap))
	for dep := range depMap {
		deps = append(deps, dep)
	}
	sort.Strings(deps)
	return deps
}

func analyzeWholeProgFuncDepsRecursive(coverPkgSet map[string]struct{}, n *callgraph.Node, depMap map[string]struct{}, seenMap map[*callgraph.Node]struct{}) {
	fn := n.Func

	if fn != nil && fn.Pkg != nil && fn.Pkg.Pkg != nil {
		if _, ok := coverPkgSet[fn.Pkg.Pkg.Path()]; ok {
			depMap[normalizeFuncName(fn)] = struct{}{}
			return
		}
	}

	path := pkgPath(fn)

	if isRuntimePackage(path) {
		return
	}
	if isHTTPPackage(path) {
		return
	}
	if isGRPCGoPackage(path) {
		return
	}
	for _, out := range n.Out {
		callee := out.Callee
		if _, exists := seenMap[callee]; exists {
			continue
		}
		seenMap[callee] = struct{}{}
		analyzeWholeProgFuncDepsRecursive(coverPkgSet, callee, depMap, seenMap)
	}
}

// mergeDeps merges two dependency slices, deduplicating entries.
func mergeDeps(existing, newDeps []string) []string {
	if len(existing) == 0 {
		return newDeps
	}
	set := make(map[string]struct{}, len(existing)+len(newDeps))
	for _, d := range existing {
		set[d] = struct{}{}
	}
	for _, d := range newDeps {
		set[d] = struct{}{}
	}
	result := make([]string, 0, len(set))
	for d := range set {
		result = append(result, d)
	}
	sort.Strings(result)
	return result
}

func pkgPath(fn *ssa.Function) string {
	if fn == nil {
		return ""
	}
	if fn.Pkg == nil {
		return ""
	}
	if fn.Pkg.Pkg == nil {
		return ""
	}
	return fn.Pkg.Pkg.Path()
}

func isRuntimePackage(pkgPath string) bool {
	return pkgPath == "runtime" || strings.HasPrefix(pkgPath, "runtime/") || strings.HasPrefix(pkgPath, "internal/runtime/")
}

func isHTTPPackage(pkgPath string) bool {
	return pkgPath == "net/http"
}

func isGRPCGoPackage(pkgPath string) bool {
	return strings.HasPrefix(pkgPath, "google.golang.org/grpc")
}
