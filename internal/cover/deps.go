// Dependency analysis for coverage instrumentation.
//
// # Architecture
//
// Tobari's dependency analysis is split into two phases:
//
//  1. Per-package lightweight analysis (createLightweightFuncInfo):
//     Each non-main package records its function names and positions using
//     only go/types (no SSA). This is fast and runs during the cover tool
//     invocation for every instrumented package. Each package also writes
//     its path and original source file paths to a temp directory so the
//     main package can later discover all coverage targets.
//
//  2. Whole-program RTA analysis (CreateMainDeps):
//     When the main package (or testmain for non-main test packages) is
//     instrumented, CreateMainDeps runs `go list -deps -json .` to collect
//     all dependency metadata. No -export or -toolexec is needed: the
//     driver only uses source-level metadata (Dir, ImportPath, GoFiles,
//     Imports), not compiled artifacts. The results are written to temp files and
//     served to packages.Load via a custom GOPACKAGESDRIVER. packages.Load
//     type-checks all packages from source with consistent type references.
//     ssautil.AllPackages + prog.Build() constructs SSA with bodies for
//     every package. Finally, RTA produces the call graph, and a VTA graph
//     confirms its dynamic edges during dependency extraction (see
//     followEdge).
//
// # Why GOPACKAGESDRIVER?
//
// When NeedTypes is requested, packages.Load normally runs `go list -export`
// internally. The -export flag causes go list to compile every package and
// produce .a files containing export data (type information). packages.Load
// then reads types from these .a files for dependency packages, avoiding
// source re-parsing — the same optimization the Go compiler uses.
//
// However, since RTA requires SSA bodies for ALL packages (not just root
// packages), we request NeedSyntax for every package. This forces
// packages.Load to parse source files regardless, making the .a-based
// type loading optimization ineffective. The compilation triggered by
// -export becomes pure overhead.
//
// By providing a custom GOPACKAGESDRIVER that serves `go list -deps -json`
// output (no -export, no compilation), we skip the unnecessary compilation
// while still providing all the metadata packages.Load needs to parse and
// type-check from source.
//
// # Why build SSA for ALL packages?
//
// RTA traces call edges through function bodies. If a third-party package
// acts as a "bridge" (e.g., passing callbacks or dispatching interface
// methods), its SSA body must exist for RTA to discover the edges. Building
// SSA only for coverage targets would miss these cross-package dependencies.
//
// For example: handler.Handle → extlib.Process(data, store.Save) → fn(data)
// Without extlib's SSA body, RTA cannot see the indirect call fn(data),
// so the dependency handler.Handle → store.Save is lost.
//
// # Temp file organization
//
// Cover package cache (global, persists across builds):
//
//	$TMPDIR/tobari/coverpkgs/<hash(dir)> — package path and directory
//	of coverage target. Keyed by SHA256 of the package's source
//	directory path. Persists across Go build cache hits.
//
// Supplementary deps (per-package, per build):
//
//	$WORK/bNNN/tobari_suppdeps.json — JSON dependency map written by the
//	cover tool and read by the compile tool to inject the data into the
//	binary via go:linkname. Each package gets its own file in its own
//	$WORK/bNNN/ build directory, so parallel builds do not race.
package cover

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/callgraph"
	"golang.org/x/tools/go/callgraph/cha"
	"golang.org/x/tools/go/callgraph/rta"
	"golang.org/x/tools/go/callgraph/vta"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"

	"github.com/goccy/tobari/internal/utils"
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
	// PendingRanges holds positions of range expressions whose type could not
	// be resolved during the cover phase (external package types). These are
	// wrapped with _maybeRangeChan for runtime channel detection.
	PendingRanges map[funcPos]struct{}
	// Fset and ParsedFiles hold the parsed AST from createLightweightFuncInfo
	// so that annotateFile can reuse them without re-parsing.
	Fset        *token.FileSet
	ParsedFiles map[string]*ast.File // filename → parsed AST
}

// createLightweightFuncInfo builds FuncNames and ChanRanges using go/parser
// and go/types directly, avoiding the overhead of packages.Load (which spawns
// go list). Function names are constructed from AST and pkgcfg.PkgPath.
// Channel range detection uses go/types with a best-effort importer that reads
// export data from compiled .a files (no subprocess needed).
// DepMap contains each function name as key with nil value,
// which is sufficient for renderMetadata's existence check.
// Dependency information is populated later via whole-program analysis at main package time.
func createLightweightFuncInfo(pkgcfg *PackageConfig, inputFiles []string) (*FunctionDependency, error) {
	// Parse the input files directly — the Go toolchain passes all .go files
	// for the package as arguments to the cover tool.
	fset := token.NewFileSet()
	var files []*ast.File
	parsedFiles := make(map[string]*ast.File, len(inputFiles))
	for _, filePath := range inputFiles {
		f, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
		if err != nil {
			return nil, fmt.Errorf("failed to parse %s: %w", filePath, err)
		}
		files = append(files, f)
		parsedFiles[filepath.Clean(filePath)] = f
	}

	// Type-check with a stub importer for channel range detection.
	// The stub returns empty packages, which is sufficient for types defined
	// locally. For external types, range expressions are left unresolved and
	// wrapped with _maybeRangeChan for runtime channel detection via reflect.
	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue),
	}
	typesConf := &types.Config{
		Importer: stubImporter{},
		Error:    func(err error) {},
	}
	// The returned package is unused; we call Check solely to populate info.Types
	// for channel range detection below. Errors are expected (stub imports) and ignored.
	_, _ = typesConf.Check(pkgcfg.PkgPath, fset, files, info)

	depMap := make(map[string][]string)
	funcNames := make(map[funcPos]string)
	chanRanges := make(map[funcPos]struct{})
	pendingRanges := make(map[funcPos]struct{})

	globalAnonIdx := 1
	// SSA always creates a synthetic init function (named "init") for
	// package-level variable initialization. Explicit init() functions
	// declared by the programmer are numbered init#1, init#2, etc.
	initCount := 1
	for _, file := range files {
		var curAnon *anonState

		ast.Inspect(file, func(n ast.Node) bool {
			switch decl := n.(type) {
			case *ast.FuncDecl:
				if decl.Name.Name == "_" || decl.Body == nil {
					return false
				}
				fqdn := buildFuncNameFromAST(pkgcfg.PkgPath, decl)
				if decl.Name.Name == "init" && decl.Recv == nil {
					fqdn = fmt.Sprintf("%s.init#%d", pkgcfg.PkgPath, initCount)
					initCount++
				}
				pos := fset.Position(decl.Name.Pos())
				funcNames[funcPos{
					Filename: filepath.Clean(pos.Filename),
					Offset:   pos.Offset,
				}] = fqdn
				depMap[fqdn] = nil

				// Collect anonymous functions within this FuncDecl
				curAnon = &anonState{parentName: fqdn, nextIdx: 1}
				collectAnonymFuncsFromInfo(fset, file, decl.Body, curAnon, funcNames, depMap)
				curAnon = nil
				return false // already walked the body

			case *ast.FuncLit:
				// Top-level FuncLit (outside any FuncDecl), e.g. var f = func() {}
				if curAnon == nil {
					name := fmt.Sprintf("%s.init$%d", pkgcfg.PkgPath, globalAnonIdx)
					globalAnonIdx++
					pos := fset.Position(decl.Pos())
					funcNames[funcPos{
						Filename: filepath.Clean(pos.Filename),
						Offset:   pos.Offset,
					}] = name
					depMap[name] = nil
				}
			}
			return true
		})

		// Separate walk for range-over-channel detection.
		// This must be a full walk (not skipped by FuncDecl's return false).
		ast.Inspect(file, func(n ast.Node) bool {
			rs, ok := n.(*ast.RangeStmt)
			if !ok || rs.X == nil {
				return true
			}
			if tv, ok := info.Types[rs.X]; ok {
				if _, isChan := tv.Type.Underlying().(*types.Chan); isChan {
					pos := fset.Position(rs.X.Pos())
					chanRanges[funcPos{
						Filename: filepath.Clean(pos.Filename),
						Offset:   pos.Offset,
					}] = struct{}{}
				}
			} else {
				// Type not resolved (external package type).
				// Mark for runtime channel detection via _maybeRangeChan.
				pos := fset.Position(rs.X.Pos())
				pendingRanges[funcPos{
					Filename: filepath.Clean(pos.Filename),
					Offset:   pos.Offset,
				}] = struct{}{}
			}
			return true
		})
	}

	return &FunctionDependency{
		PkgPath:       pkgcfg.PkgPath,
		DepMap:        depMap,
		FuncNames:     funcNames,
		ChanRanges:    chanRanges,
		PendingRanges: pendingRanges,
		Fset:          fset,
		ParsedFiles:   parsedFiles,
	}, nil
}

// buildFuncNameFromAST constructs a fully qualified function name from AST.
func buildFuncNameFromAST(pkgPath string, decl *ast.FuncDecl) string {
	if decl.Recv == nil || len(decl.Recv.List) == 0 {
		return pkgPath + "." + decl.Name.Name
	}
	recv := decl.Recv.List[0].Type
	star := false
	if starExpr, ok := recv.(*ast.StarExpr); ok {
		star = true
		recv = starExpr.X
	}
	typeName := recvTypeString(recv)
	if star {
		return "(*" + pkgPath + "." + typeName + ")." + decl.Name.Name
	}
	return "(" + pkgPath + "." + typeName + ")." + decl.Name.Name
}

// recvTypeString returns the string representation of a receiver type expression.
func recvTypeString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.IndexExpr:
		return recvTypeString(t.X) + "[" + recvTypeString(t.Index) + "]"
	case *ast.IndexListExpr:
		parts := make([]string, len(t.Indices))
		for i, idx := range t.Indices {
			parts[i] = recvTypeString(idx)
		}
		return recvTypeString(t.X) + "[" + strings.Join(parts, ", ") + "]"
	default:
		return fmt.Sprintf("%v", expr)
	}
}

// stubImporter returns empty packages for all imports.
// This allows type-checking to resolve local types (including channels)
// without needing compiled dependency packages.
type stubImporter struct{}

func (stubImporter) Import(path string) (*types.Package, error) {
	return types.NewPackage(path, ""), nil
}

type anonState struct {
	parentName string
	nextIdx    int
}

// collectAnonymFuncsFromInfo walks a function body and registers all nested FuncLit
// nodes with their SSA-compatible names (parent$1, parent$2, etc.).
func collectAnonymFuncsFromInfo(fset *token.FileSet, file *ast.File, body *ast.BlockStmt, state *anonState, funcNames map[funcPos]string, depMap map[string][]string) {
	ast.Inspect(body, func(n ast.Node) bool {
		lit, ok := n.(*ast.FuncLit)
		if !ok {
			return true
		}
		name := fmt.Sprintf("%s$%d", state.parentName, state.nextIdx)
		state.nextIdx++
		pos := fset.Position(lit.Pos())
		funcNames[funcPos{
			Filename: filepath.Clean(pos.Filename),
			Offset:   pos.Offset,
		}] = name
		depMap[name] = nil

		// Nested anonymous functions inherit the new parent name
		innerState := &anonState{parentName: name, nextIdx: 1}
		collectAnonymFuncsFromInfo(fset, file, lit.Body, innerState, funcNames, depMap)
		return false // already walked nested funcs
	})
}

// CreateMainDeps performs whole-program SSA analysis starting from the main
// package (or test binary) and returns dependency maps for all coverage-target
// packages. It uses RTA (Rapid Type Analysis) for call graph construction
// and reachability, refined by VTA (variable type analysis) for dynamic call
// edges during dependency extraction (see followEdge).
//
// It runs `go list -deps -json .` to collect package metadata (no -export
// or -toolexec needed), then serves the results to packages.Load via a
// custom GOPACKAGESDRIVER. The driver determines coverage-target packages
// by checking the global cache written by recordCoverPkg. All packages are
// parsed from source and type-checked, then SSA is built for every package
// so that RTA can trace call edges through third-party libraries.
// For test mode, -test is added to go list to include test dependencies.
// Returns nil if no coverage-target packages are found.
//
// excludeAnalysis lists package-path prefixes whose SSA bodies are not built,
// making RTA treat them as opaque leaves. This is a caller assertion that the
// excluded packages never call back into coverage-target code (see the
// --exclude-analysis flag). It exists because whole-program RTA is superlinear
// in the number of reachable types and interface call sites: on large services
// the generated gRPC/protobuf client packages dominate the analysis while
// contributing no cover→cover edges.
func CreateMainDeps(mainSourceFiles []string, isTestMode bool, testPkgCfg *PackageConfig, excludeAnalysis []string) (map[string][]string, error) {
	tobariBin, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("failed to get tobari binary path: %w", err)
	}

	dir, err := resolveGoListDir(mainSourceFiles, testPkgCfg)
	if err != nil {
		// For testmain with no non-main cover targets, the global cache is
		// empty and directory resolution fails. This is normal — no suppDeps needed.
		if testPkgCfg != nil {
			return nil, nil
		}
		return nil, err
	}

	goListJSON, err := utils.GoListDepsJSON(dir, isTestMode, ".")
	if err != nil {
		return nil, fmt.Errorf("failed to run go list: %w", err)
	}

	// Determine coverage-target packages from the go list output by checking
	// the global cache written by recordCoverPkg. This works even when Go's
	// build cache hits and the cover tool is not re-invoked.
	coverResult, err := buildCoverPkgSet(goListJSON)
	if err != nil {
		return nil, err
	}
	if len(coverResult.pkgSet) == 0 {
		return nil, nil
	}

	// Write go list result and cover package paths to temp files for the driver.
	goListFile, err := writeTempJSON(goListJSON)
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.Remove(goListFile) }()

	coverPkgPathsFile, err := writeTempCoverPkgPaths(coverResult.pkgSet)
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.Remove(coverPkgPathsFile) }()

	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedTypes | packages.NeedSyntax |
			packages.NeedTypesInfo | packages.NeedDeps | packages.NeedImports |
			packages.NeedFiles | packages.NeedCompiledGoFiles,
		Dir:   dir,
		Tests: isTestMode,
		Env: append(utils.FilterGOFLAGSEnvs(),
			"GOPACKAGESDRIVER="+tobariBin,
			utils.EnvPackagesDriver+"=1",
			utils.EnvGoListFile+"="+goListFile,
			utils.EnvCoverPkgPathsFile+"="+coverPkgPathsFile,
		),
	}
	coverPkgSet := coverResult.pkgSet

	pkgs, err := packages.Load(cfg, ".")
	if err != nil {
		return nil, err
	}

	var pkgErrs []error
	for _, pkg := range pkgs {
		for _, e := range pkg.Errors {
			pkgErrs = append(pkgErrs, e)
		}
	}
	if len(pkgErrs) != 0 {
		return nil, errors.Join(pkgErrs...)
	}

	// Filter coverPkgSet to only actual dependencies.
	allDepPaths := make(map[string]struct{})
	packages.Visit(pkgs, nil, func(p *packages.Package) {
		if p.Types != nil {
			allDepPaths[p.PkgPath] = struct{}{}
		}
	})
	for pkg := range coverPkgSet {
		if _, ok := allDepPaths[pkg]; !ok {
			delete(coverPkgSet, pkg)
		}
	}

	prog, _ := ssautil.AllPackages(pkgs, ssa.InstantiateGenerics)

	if len(excludeAnalysis) == 0 {
		// Build SSA for ALL packages.
		prog.Build()
	} else {
		// Build SSA for every package except the excluded ones. Excluded
		// packages stay bodyless, so RTA cannot trace calls through them; the
		// caller asserts they never re-enter cover code.
		for _, ssaPkg := range prog.AllPackages() {
			if ssaPkg.Pkg == nil {
				continue
			}
			if !isMainPkg(ssaPkg) && isExcludedFromAnalysis(ssaPkg.Pkg.Path(), excludeAnalysis, coverPkgSet) {
				continue
			}
			ssaPkg.Build()
		}
	}

	// Collect RTA roots from main/test packages and cover-target packages.
	//
	// The iteration order must be deterministic: prog.AllPackages() and
	// ssa.Package.Members are backed by maps, and rta.Analyze gives roots[0]
	// special treatment (callgraph.New(roots[0]) is the only place a node is
	// created for a root that has no call edges). An unstable order therefore
	// makes edge-less roots appear in, or vanish from, the resulting suppDeps.
	ssaPkgs := prog.AllPackages()
	sort.Slice(ssaPkgs, func(i, j int) bool {
		return ssaPkgPath(ssaPkgs[i]) < ssaPkgPath(ssaPkgs[j])
	})

	var roots []*ssa.Function
	for _, ssaPkg := range ssaPkgs {
		if ssaPkg.Pkg == nil {
			continue
		}
		pkgPath := ssaPkg.Pkg.Path()
		_, isCoverTarget := coverPkgSet[pkgPath]
		if !isMainOrTestPkg(pkgPath) && !isMainPkg(ssaPkg) && !isCoverTarget {
			continue
		}
		if mainFunc := ssaPkg.Func("main"); mainFunc != nil {
			roots = append(roots, mainFunc)
		}
		if initFunc := ssaPkg.Func("init"); initFunc != nil {
			roots = append(roots, initFunc)
		}
		names := make([]string, 0, len(ssaPkg.Members))
		for name := range ssaPkg.Members {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			fn, ok := ssaPkg.Members[name].(*ssa.Function)
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
	// No entry points (main/init/Test*) means RTA has nothing to trace from,
	// so no supplementary deps can be produced. Treat this as a no-op rather
	// than a hard error — the build should still succeed.
	if len(roots) == 0 {
		return nil, nil
	}

	rtaResult := rta.Analyze(roots, true)
	graph := rtaResult.CallGraph

	// RTA resolves dynamic calls far too broadly for scoped coverage: a
	// function-value call (e.g. sync.Once's f()) is resolved purely by
	// signature — every address-taken function with a matching signature
	// becomes a callee, fanning one shared-helper call site out to hundreds
	// of unrelated functions, including test and Example bodies — and
	// interface dispatch is resolved to every implementation in the binary.
	//
	// To keep the reachability judgment sound, build a VTA call graph over
	// the RTA-reachable functions and use it during dependency extraction to
	// confirm dynamic edges (both function-value calls and interface
	// dispatch): VTA propagates which values actually flow into each call
	// site, so a callback or implementation passed from user code is kept
	// while signature-only and every-implementation matches are dropped.
	// Static calls, and invoke edges whose callee is a generic
	// instantiation (a VTA blind spot), stay resolved by RTA — see
	// followEdge for the exact policy.
	vtaFuncs := make(map[*ssa.Function]bool, len(rtaResult.Reachable))
	for fn := range rtaResult.Reachable {
		vtaFuncs[fn] = true
	}
	vtaGraph := vta.CallGraph(vtaFuncs, cha.CallGraph(prog))

	followable := newFollowableEdges(graph, vtaGraph)
	frontierSets := newFrontiers(graph, followable, coverPkgSet)

	// Build dependency map for coverage-target packages.
	//
	// Iterate the reachable set rather than the call graph's nodes: RTA only
	// creates a graph node for a function that participates in a call edge (plus
	// roots[0], which callgraph.New materializes). A reachable cover function
	// with no cover-relevant edges would otherwise be included or omitted
	// depending on where it happened to land in the root list.
	suppDeps := make(map[string][]string)
	for fn := range rtaResult.Reachable {
		if funcCoverPkgPath(fn, coverPkgSet) == "" {
			continue
		}
		fnName := normalizeFuncName(fn)
		var deps []string
		if n := graph.Nodes[fn]; n != nil {
			deps = frontierSets.depsFrom(n, followable)
		}
		suppDeps[fnName] = mergeDeps(suppDeps[fnName], deps)
	}

	return suppDeps, nil
}

// followEdge reports whether a call edge from the RTA graph should be
// followed during dependency extraction.
//
// Static calls are always followed. Dynamic calls — interface dispatch
// (invoke mode) and function-value calls — need confirmation from the VTA
// graph, which propagates the values that actually flow into each call site.
// RTA alone resolves a function-value call to every address-taken function
// with a matching signature (e.g. sync.Once's f() fans out to every func()
// in the binary, including test and Example bodies), and resolves interface
// dispatch to every implementation in the binary (e.g. err.Error() inside
// fmt reaches every error type), so following RTA's dynamic edges directly
// drags provably unreachable code into every caller's dependency closure.
//
// The exception is invoke edges whose callee is an instantiation of a
// generic function or method: VTA does not track type flows into interface
// values for instantiated generics (it resolves such call sites only to
// their non-generic implementations), so requiring VTA confirmation there
// would silently drop real dependencies. For those callees RTA's judgment
// is kept.
func followEdge(vtaEdges map[vtaEdge]struct{}, e *callgraph.Edge) bool {
	if followedWithoutVTA(e.Site, e.Callee.Func) {
		return true
	}
	_, confirmed := vtaEdges[vtaEdge{site: e.Site, callee: e.Callee.Func}]
	return confirmed
}

// followedWithoutVTA reports whether the policy above resolves a (site,
// callee) pair to "follow" without consulting the VTA index.
//
// indexVTAEdges shares this predicate rather than repeating the
// short-circuits, so a later change to the policy cannot leave the index
// missing a key the policy still asks about.
func followedWithoutVTA(site ssa.CallInstruction, callee *ssa.Function) bool {
	if site == nil {
		return true
	}
	common := site.Common()
	if common.StaticCallee() != nil {
		return true
	}
	return common.IsInvoke() && callee != nil && callee.Origin() != nil
}

// vtaEdge identifies a call the VTA graph confirms: a call site paired with
// one callee the values flowing into that site can reach.
type vtaEdge struct {
	site   ssa.CallInstruction
	callee *ssa.Function
}

// indexVTAEdges collects the VTA call site and callee pairs that followEdge
// needs confirmation for.
//
// Pairs the policy resolves on its own are left out: indexing them too would
// add an entry per static call, which no lookup ever reads, inside the
// compiler's memory budget.
func indexVTAEdges(vtaGraph *callgraph.Graph) map[vtaEdge]struct{} {
	edges := make(map[vtaEdge]struct{}, len(vtaGraph.Nodes))
	for _, n := range vtaGraph.Nodes {
		for _, e := range n.Out {
			if e.Site == nil || e.Callee == nil {
				continue
			}
			if followedWithoutVTA(e.Site, e.Callee.Func) {
				continue
			}
			edges[vtaEdge{site: e.Site, callee: e.Callee.Func}] = struct{}{}
		}
	}
	return edges
}

// followableEdges holds, per RTA node, the out-edges the dependency traversal
// may follow.
type followableEdges struct {
	out map[*callgraph.Node][]*callgraph.Edge
}

// newFollowableEdges applies the edge policy to every RTA edge once.
//
// followEdge depends only on the edge, so a traversal that asks per visit gets
// the same verdict it got for every other coverage-target function that
// reached the edge.
func newFollowableEdges(rtaGraph, vtaGraph *callgraph.Graph) *followableEdges {
	vtaEdges := indexVTAEdges(vtaGraph)
	out := make(map[*callgraph.Node][]*callgraph.Edge, len(rtaGraph.Nodes))

	// kept is scratch space so each stored slice can be allocated at its exact
	// length; a per-node slice of capacity len(n.Out) would keep the pruned
	// fan-out resident.
	var kept []*callgraph.Edge
	for _, n := range rtaGraph.Nodes {
		kept = kept[:0]
		for _, e := range n.Out {
			if followEdge(vtaEdges, e) {
				kept = append(kept, e)
			}
		}
		if len(kept) == len(n.Out) {
			// RTA is done building by now and the traversal only reads these,
			// so aliasing beats a defensive copy.
			out[n] = n.Out
			continue
		}
		followable := make([]*callgraph.Edge, len(kept))
		copy(followable, kept)
		out[n] = followable
	}
	return &followableEdges{out: out}
}

// from returns the out-edges of n that may be followed.
func (f *followableEdges) from(n *callgraph.Node) []*callgraph.Edge {
	return f.out[n]
}

// isExcludedFromAnalysis reports whether a package's SSA body may be skipped.
//
// A package is excluded only when it matches one of the caller's prefixes AND
// it is not needed to produce the dependency map. Coverage-target packages and
// the main/test entry points are never excluded, even if a prefix matches them:
// they are the source of the cover→cover edges the analysis exists to find, and
// dropping their bodies would silently shrink coverage denominators. Since
// --exclude-analysis is meant for packages irrelevant to the analysis, naming a
// coverage target is a user mistake that is ignored rather than honored.
func isExcludedFromAnalysis(pkgPath string, excludeAnalysis []string, coverPkgSet map[string]struct{}) bool {
	if !utils.MatchesPkgPrefix(pkgPath, excludeAnalysis) {
		return false
	}
	if isMainOrTestPkg(pkgPath) {
		return false
	}
	// matchCoverPkg also resolves test variants ("pkg [pkg.test]").
	return matchCoverPkg(pkgPath, coverPkgSet) == ""
}

// isMainOrTestPkg reports whether a package path denotes the main package or a
// test binary / test variant package.
//
// Note that a main package's import path is normally its module path, not the
// literal "main"; use isMainPkg when an *ssa.Package is available.
func isMainOrTestPkg(pkgPath string) bool {
	return pkgPath == "main" ||
		strings.HasSuffix(pkgPath, ".test") ||
		strings.Contains(pkgPath, " [")
}

// isMainPkg reports whether an SSA package is a main package. This checks the
// package name because `go list` reports a main package's import path as its
// module path (e.g. "example.com/app"), not "main".
func isMainPkg(ssaPkg *ssa.Package) bool {
	return ssaPkg.Pkg != nil && ssaPkg.Pkg.Name() == "main"
}

// ssaPkgPath returns an SSA package's import path, or "" for the synthetic
// package with no types.Package. Used to sort packages deterministically.
func ssaPkgPath(ssaPkg *ssa.Package) string {
	if ssaPkg.Pkg == nil {
		return ""
	}
	return ssaPkg.Pkg.Path()
}

// coverPkgSetResult holds cover package paths discovered from the global cache.
type coverPkgSetResult struct {
	pkgSet map[string]struct{}
}

// buildCoverPkgSet parses go list JSON output and checks the global cache
// to determine which packages are coverage targets.
func buildCoverPkgSet(goListJSON []byte) (*coverPkgSetResult, error) {
	type goListPkg struct {
		Dir string
	}
	result := &coverPkgSetResult{pkgSet: make(map[string]struct{})}
	decoder := json.NewDecoder(bytes.NewReader(goListJSON))
	for decoder.More() {
		var pkg goListPkg
		if err := decoder.Decode(&pkg); err != nil {
			return nil, fmt.Errorf("failed to decode go list entry: %w", err)
		}
		if cache := lookupCoverPkgByDir(pkg.Dir); cache != nil {
			result.pkgSet[cache.PkgPath] = struct{}{}
		}
	}
	return result, nil
}

// writeTempCoverPkgPaths writes cover package paths to a temp file for the driver.
func writeTempCoverPkgPaths(coverPkgSet map[string]struct{}) (string, error) {
	paths := make([]string, 0, len(coverPkgSet))
	for p := range coverPkgSet {
		paths = append(paths, p)
	}
	data, err := json.Marshal(paths)
	if err != nil {
		return "", err
	}
	return writeTempJSON(data)
}

// resolveGoListDir determines the directory for running go list.
// For main packages: uses the directory of the first source file.
// For testmain: scans the global cover package cache, scoped by
// ModulePath from pkgcfg, to find a cover target's directory.
func resolveGoListDir(mainSourceFiles []string, testPkgCfg *PackageConfig) (string, error) {
	if len(mainSourceFiles) > 0 {
		return filepath.Dir(mainSourceFiles[0]), nil
	}
	if testPkgCfg != nil {
		lookupPath := strings.TrimSuffix(testPkgCfg.PkgPath, ".test")
		modulePath := testPkgCfg.ModulePath

		cacheDir := utils.CoverPkgsDir()
		entries, err := os.ReadDir(cacheDir)
		if err != nil {
			return "", fmt.Errorf("no cover package cache found: %w", err)
		}
		// Single pass: look for exact match and collect a fallback directory.
		var fallbackDir string
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			data, err := os.ReadFile(filepath.Join(cacheDir, entry.Name()))
			if err != nil {
				continue
			}
			var cache coverPkgCache
			if err := json.Unmarshal(data, &cache); err != nil || cache.Dir == "" {
				continue
			}
			// Match on package-path boundaries. A plain prefix test would let
			// module "example.com/foo" claim the cover targets of the unrelated
			// module "example.com/foobar", whose entries share the same global
			// cache. Picking a foreign directory then yields a bogus go list dir.
			if modulePath != "" && !utils.MatchesPkgPrefix(cache.PkgPath, []string{modulePath}) {
				continue
			}
			if cache.PkgPath == lookupPath {
				return cache.Dir, nil
			}
			if fallbackDir == "" {
				fallbackDir = cache.Dir
			}
		}
		// No exact match: the test package is a main package. Derive the
		// test package directory from a same-module cover target.
		if fallbackDir != "" {
			if d := findModuleRootAndDerive(fallbackDir, modulePath, lookupPath); d != "" {
				return d, nil
			}
		}
		return "", fmt.Errorf("cannot determine test package directory: pkgPath=%q, modulePath=%q", testPkgCfg.PkgPath, modulePath)
	}
	return "", fmt.Errorf("cannot determine package directory: mainSourceFiles=%v", mainSourceFiles)
}

// findModuleRootAndDerive walks up from dir to find go.mod, then derives
// the target package directory using moduleRoot + (targetPath - modulePath).
func findModuleRootAndDerive(dir, modulePath, targetPath string) string {
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			rel := strings.TrimPrefix(targetPath, modulePath)
			rel = strings.TrimPrefix(rel, "/")
			return filepath.Join(dir, rel)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// writeTempJSON writes data to a temporary file and returns the path.
func writeTempJSON(data []byte) (string, error) {
	f, err := os.CreateTemp("", utils.TmpDriverJSONPattern)
	if err != nil {
		return "", err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return f.Name(), nil
}

// normalizeFuncName returns the non-instantiated function name for generic
// functions (using Origin()), or the plain name for non-generic functions.
// This ensures SSA names like "(*pkg.Result[int]).IsOk" are normalized to
// "(*pkg.Result[T]).IsOk" to match per-package metadata from go/types.
// For test variant packages (e.g., "pkg [pkg.test]"), the variant suffix
// is stripped so names match the cover tool's funcMap (which uses base paths).
func normalizeFuncName(fn *ssa.Function) string {
	name := fn.String()
	if origin := fn.Origin(); origin != nil {
		name = origin.String()
	}
	return stripTestVariant(name)
}

// stripTestVariant removes test variant suffixes like " [pkg.test]" from
// function names. E.g., "example.com/pkg [example.com/pkg.test].Func" becomes
// "example.com/pkg.Func".
func stripTestVariant(name string) string {
	idx := strings.Index(name, " [")
	if idx < 0 {
		return name
	}
	end := strings.Index(name[idx:], "]")
	if end < 0 {
		return name
	}
	return name[:idx] + name[idx+end+1:]
}

// funcCoverPkgPath returns the package path if fn belongs to a coverage-target
// package. For instantiated generic functions (where Pkg is nil), it checks the
// Origin function's package instead. For test variant packages (e.g.,
// "pkg [pkg.test]"), the base path before " [" is checked against coverPkgSet.
func funcCoverPkgPath(fn *ssa.Function, coverPkgSet map[string]struct{}) string {
	if fn.Pkg != nil && fn.Pkg.Pkg != nil {
		if path := matchCoverPkg(fn.Pkg.Pkg.Path(), coverPkgSet); path != "" {
			return path
		}
	}
	if origin := fn.Origin(); origin != nil && origin.Pkg != nil && origin.Pkg.Pkg != nil {
		if path := matchCoverPkg(origin.Pkg.Pkg.Path(), coverPkgSet); path != "" {
			return path
		}
	}
	return ""
}

// matchCoverPkg checks if pkgPath (or its base path for test variants) is in coverPkgSet.
func matchCoverPkg(pkgPath string, coverPkgSet map[string]struct{}) string {
	if _, ok := coverPkgSet[pkgPath]; ok {
		return pkgPath
	}
	// Test variant packages have paths like "pkg [pkg.test]".
	if idx := strings.Index(pkgPath, " ["); idx >= 0 {
		basePath := pkgPath[:idx]
		if _, ok := coverPkgSet[basePath]; ok {
			return basePath
		}
	}
	return ""
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
