// Dependency analysis for coverage instrumentation.
//
// # Architecture
//
// Tobari's dependency analysis is split into two phases to balance build
// speed with analysis precision:
//
//  1. Per-package lightweight analysis (createLightweightFuncInfo):
//     Each non-main package records its function names and positions using
//     only go/types (no SSA). This is fast and runs during the cover tool
//     invocation for every instrumented package. Each package also writes
//     its package path to a temp directory keyed by the parent process ID
//     (ppid), so the main package can later discover all coverage targets.
//
//  2. Whole-program RTA analysis at main package (CreateMainDeps):
//     When the main package is instrumented, it reads the recorded package
//     paths (via ppid-keyed temp files) and performs whole-program SSA
//     analysis using RTA (Rapid Type Analysis). This produces an accurate
//     call graph that correctly handles generics, interfaces, and cross-
//     package calls. The resulting dependency map is written as JSON and
//     injected into the binary via go:linkname (AddSupplementaryDeps).
//
// # Why ppid-based temp files?
//
// The Go toolchain invokes the cover tool as a separate process for each
// package. These invocations share the same parent process (the `go`
// command), so os.Getppid() identifies the build session. This allows
// non-main packages to record their paths and the main package to read
// them, without any shared state or IPC mechanism.
//
// When multiple `go test` commands run concurrently, each has a different
// ppid, so their temp files are naturally isolated. Within a single
// `go test ./...`, all packages share the same ppid. The main package
// filters the recorded paths to only its actual dependencies via the SSA
// program's package list, so unrelated packages in the same session do
// not affect the analysis result.
package cover

import (
	"errors"
	"fmt"
	"go/ast"
	"go/types"
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

// funcPos identifies a function by its source position (file + byte offset).
type funcPos struct {
	Filename string
	Offset   int
}

type FunctionDependency struct {
	PkgPath string
	DepMap  map[string][]string
	// FuncNames maps source position → fully qualified function name for all functions in the target package.
	FuncNames map[funcPos]string
	// ChanRanges holds positions of range expressions over channels (for v := range ch).
	ChanRanges map[funcPos]struct{}
}

// createLightweightFuncInfo builds FuncNames and ChanRanges using only go/types
// (no SSA analysis). DepMap contains each function name as key with nil value,
// which is sufficient for renderMetadata's existence check.
// Dependency information is populated later via whole-program analysis at main package time.
func createLightweightFuncInfo(pkgcfg *PackageConfig, path string) (*FunctionDependency, error) {
	dir := filepath.Dir(path)
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo,
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

	var targetPkg *packages.Package
	for _, pkg := range pkgs {
		if pkg.Name == pkgcfg.PkgName || pkg.PkgPath == pkgcfg.PkgName {
			targetPkg = pkg
			break
		}
	}
	if targetPkg == nil {
		return nil, fmt.Errorf("failed to find target package: %s", pkgcfg.PkgName)
	}

	depMap := make(map[string][]string)
	funcNames := make(map[funcPos]string)
	chanRanges := make(map[funcPos]struct{})

	globalAnonIdx := 1
	for _, file := range targetPkg.Syntax {
		var curAnon *anonState

		ast.Inspect(file, func(n ast.Node) bool {
			switch decl := n.(type) {
			case *ast.FuncDecl:
				if decl.Name.Name == "_" || decl.Body == nil {
					return false
				}
				obj := targetPkg.TypesInfo.Defs[decl.Name]
				if obj == nil {
					return true
				}
				fn, ok := obj.(*types.Func)
				if !ok {
					return true
				}
				fqdn := fn.FullName()
				pos := targetPkg.Fset.Position(decl.Name.Pos())
				funcNames[funcPos{
					Filename: filepath.Clean(pos.Filename),
					Offset:   pos.Offset,
				}] = fqdn
				depMap[fqdn] = nil

				// Collect anonymous functions within this FuncDecl
				curAnon = &anonState{parentName: fqdn, nextIdx: 1}
				collectAnonymFuncs(targetPkg, file, decl.Body, curAnon, funcNames, depMap)
				curAnon = nil
				return false // already walked the body

			case *ast.FuncLit:
				// Top-level FuncLit (outside any FuncDecl), e.g. var f = func() {}
				if curAnon == nil {
					name := fmt.Sprintf("%s.init$%d", targetPkg.PkgPath, globalAnonIdx)
					globalAnonIdx++
					pos := targetPkg.Fset.Position(decl.Pos())
					funcNames[funcPos{
						Filename: filepath.Clean(pos.Filename),
						Offset:   pos.Offset,
					}] = name
					depMap[name] = nil
				}

			case *ast.RangeStmt:
				if decl.X != nil {
					if t := targetPkg.TypesInfo.TypeOf(decl.X); t != nil {
						if _, ok := t.Underlying().(*types.Chan); ok {
							pos := targetPkg.Fset.Position(decl.X.Pos())
							chanRanges[funcPos{
								Filename: filepath.Clean(pos.Filename),
								Offset:   pos.Offset,
							}] = struct{}{}
						}
					}
				}
			}
			return true
		})
	}

	return &FunctionDependency{
		PkgPath:    targetPkg.PkgPath,
		DepMap:     depMap,
		FuncNames:  funcNames,
		ChanRanges: chanRanges,
	}, nil
}

type anonState struct {
	parentName string
	nextIdx    int
}

// collectAnonymFuncs walks a function body and registers all nested FuncLit
// nodes with their SSA-compatible names (parent$1, parent$2, etc.).
func collectAnonymFuncs(pkg *packages.Package, file *ast.File, body *ast.BlockStmt, state *anonState, funcNames map[funcPos]string, depMap map[string][]string) {
	ast.Inspect(body, func(n ast.Node) bool {
		lit, ok := n.(*ast.FuncLit)
		if !ok {
			return true
		}
		name := fmt.Sprintf("%s$%d", state.parentName, state.nextIdx)
		state.nextIdx++
		pos := pkg.Fset.Position(lit.Pos())
		funcNames[funcPos{
			Filename: filepath.Clean(pos.Filename),
			Offset:   pos.Offset,
		}] = name
		depMap[name] = nil

		// Nested anonymous functions inherit the new parent name
		innerState := &anonState{parentName: name, nextIdx: 1}
		collectAnonymFuncs(pkg, file, lit.Body, innerState, funcNames, depMap)
		return false // already walked nested funcs
	})
}

// filterGOFLAGS removes GOFLAGS from environment variables to prevent
// recursive toolexec invocations.
func filterGOFLAGS(envs []string) []string {
	newEnvs := make([]string, 0, len(envs))
	for _, kv := range envs {
		i := strings.IndexByte(kv, '=')
		if i >= 0 && kv[:i] == "GOFLAGS" {
			continue
		}
		newEnvs = append(newEnvs, kv)
	}
	return newEnvs
}

// CreateMainDeps performs whole-program SSA analysis starting from the main
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
func CreateMainDeps(mainFilePath string, coverPkgs []string) (map[string][]string, error) {
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
			if strings.HasPrefix(name, "Test") && fn.Pos().IsValid() {
				if pos := prog.Fset.Position(fn.Pos()); strings.HasSuffix(pos.Filename, "_test.go") {
					roots = append(roots, fn)
				}
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
		deps := analyzeMainFuncDeps(coverPkgSet, n)
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

// analyzeMainFuncDeps finds all functions in coverage-target packages that
// are transitively reachable from node n's callees.
func analyzeMainFuncDeps(coverPkgSet map[string]struct{}, n *callgraph.Node) []string {
	depMap := make(map[string]struct{})
	seenMap := make(map[*callgraph.Node]struct{})
	for _, out := range n.Out {
		callee := out.Callee
		if _, exists := seenMap[callee]; exists {
			continue
		}
		seenMap[callee] = struct{}{}
		analyzeMainFuncDepsRecursive(coverPkgSet, callee, depMap, seenMap)
	}
	deps := make([]string, 0, len(depMap))
	for dep := range depMap {
		deps = append(deps, dep)
	}
	sort.Strings(deps)
	return deps
}

func analyzeMainFuncDepsRecursive(coverPkgSet map[string]struct{}, n *callgraph.Node, depMap map[string]struct{}, seenMap map[*callgraph.Node]struct{}) {
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
		analyzeMainFuncDepsRecursive(coverPkgSet, callee, depMap, seenMap)
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
