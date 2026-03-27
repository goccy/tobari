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
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

// CreateWholeProgDeps performs whole-program SSA analysis starting from the main
// package and returns dependency maps for all coverage-target packages.
// It uses RTA (Rapid Type Analysis) for call graph construction.
//
// The package is loaded with Tests=true so that _test.go files are included.
// This allows RTA to discover concrete types created in test code
// (e.g., types implementing interfaces) without requiring any changes to
// main.go. For non-test builds (go build/go run), Tests=true has no effect
// when no _test.go files exist.
//
// RTA roots include main(), init(), and all Test* functions found in the
// loaded packages, which is equivalent to using testmain.go's main() as root.
func CreateWholeProgDeps(mainFilePath string, coverPkgs []string) (map[string][]string, error) {
	coverPkgSet := make(map[string]struct{}, len(coverPkgs))
	for _, p := range coverPkgs {
		coverPkgSet[p] = struct{}{}
	}

	dir := filepath.Dir(mainFilePath)
	cfg := &packages.Config{
		Mode:  packages.LoadAllSyntax,
		Dir:   dir,
		Tests: true,
		Env:   filterGOFLAGS(os.Environ()),
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

	prog, _ := ssautil.AllPackages(pkgs, ssa.InstantiateGenerics)
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

	// Collect RTA roots from all loaded packages.
	// For go build/go run: main() and init() are the roots.
	// For go test: Test* functions are also included as additional roots,
	// equivalent to testmain.go's main() calling testing.Main().
	var roots []*ssa.Function
	for _, pkg := range pkgs {
		ssaPkg := prog.Package(pkg.Types)
		if ssaPkg == nil {
			continue
		}
		if mainFunc := ssaPkg.Func("main"); mainFunc != nil {
			roots = append(roots, mainFunc)
		}
		if initFunc := ssaPkg.Func("init"); initFunc != nil {
			roots = append(roots, initFunc)
		}
		for name, member := range ssaPkg.Members {
			fn, ok := member.(*ssa.Function)
			if !ok {
				continue
			}
			if strings.HasPrefix(name, "Test") {
				roots = append(roots, fn)
			}
		}
	}
	if len(roots) == 0 {
		return nil, fmt.Errorf("no entry points found in main package")
	}

	rtaResult := rta.Analyze(roots, true)
	graph := rtaResult.CallGraph

	// Build dependency map for coverage-target packages.
	suppDeps := make(map[string][]string)
	for _, n := range graph.Nodes {
		if n.Func == nil {
			continue
		}
		if funcCoverPkgPath(n.Func, coverPkgSet) == "" {
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

// funcCoverPkgPath returns the package path if fn belongs to a coverage-target
// package. For instantiated generic functions (where Pkg is nil), it checks the
// Origin function's package instead.
func funcCoverPkgPath(fn *ssa.Function, coverPkgSet map[string]struct{}) string {
	if fn.Pkg != nil && fn.Pkg.Pkg != nil {
		if _, ok := coverPkgSet[fn.Pkg.Pkg.Path()]; ok {
			return fn.Pkg.Pkg.Path()
		}
	}
	if origin := fn.Origin(); origin != nil && origin.Pkg != nil && origin.Pkg.Pkg != nil {
		if _, ok := coverPkgSet[origin.Pkg.Pkg.Path()]; ok {
			return origin.Pkg.Pkg.Path()
		}
	}
	return ""
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

	if fn != nil && funcCoverPkgPath(fn, coverPkgSet) != "" {
		depMap[normalizeFuncName(fn)] = struct{}{}
		return
	}

	path := resolvePkgPath(fn)

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

// resolvePkgPath returns the package path for fn, falling back to Origin()
// for instantiated generic functions where Pkg is nil.
func resolvePkgPath(fn *ssa.Function) string {
	if p := pkgPath(fn); p != "" {
		return p
	}
	if fn == nil {
		return ""
	}
	if origin := fn.Origin(); origin != nil {
		return pkgPath(origin)
	}
	return ""
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
