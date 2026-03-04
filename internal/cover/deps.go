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
	"golang.org/x/tools/go/callgraph/cha"
	"golang.org/x/tools/go/callgraph/vta"
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
	// FuncNames maps source position → SSA FQDN for all functions in the target package.
	FuncNames map[funcPos]string
	// ChanRanges holds positions of range expressions over channels (for v := range ch).
	ChanRanges map[funcPos]struct{}
}

func createFunctionDependencyMap(pkgcfg *PackageConfig, path string) (*FunctionDependency, error) {
	prog, targetPkg, loadedPkgs, err := getSSAProgram(pkgcfg, path)
	if err != nil {
		return nil, err
	}

	graph := vta.CallGraph(ssautil.AllFunctions(prog), cha.CallGraph(prog))
	depMap := make(map[string][]string)
	funcNames := make(map[funcPos]string)
	for _, n := range graph.Nodes {
		if n.Func == nil || n.Func.Pkg != targetPkg {
			continue
		}
		depMap[n.Func.String()] = analyzeFuncDeps(targetPkg, n)
		if n.Func.Pos().IsValid() {
			pos := prog.Fset.Position(n.Func.Pos())
			funcNames[funcPos{
				Filename: filepath.Clean(pos.Filename),
				Offset:   pos.Offset,
			}] = n.Func.String()
		}
	}

	// Detect range-over-channel expressions using type info.
	chanRanges := make(map[funcPos]struct{})
	for _, pkg := range loadedPkgs {
		if pkg.PkgPath != targetPkg.Pkg.Path() {
			continue
		}
		for _, file := range pkg.Syntax {
			ast.Inspect(file, func(n ast.Node) bool {
				rs, ok := n.(*ast.RangeStmt)
				if !ok {
					return true
				}
				if t := pkg.TypesInfo.TypeOf(rs.X); t != nil {
					if _, ok := t.Underlying().(*types.Chan); ok {
						pos := prog.Fset.Position(rs.X.Pos())
						chanRanges[funcPos{
							Filename: filepath.Clean(pos.Filename),
							Offset:   pos.Offset,
						}] = struct{}{}
					}
				}
				return true
			})
		}
	}

	// Add functions/methods missing from the call graph.
	// This handles generic (parameterized) types whose methods are not
	// included by ssautil.AllFunctions, as well as any other unreachable
	// functions that still appear in the source AST.
	for _, member := range targetPkg.Members {
		switch m := member.(type) {
		case *ssa.Function:
			addMissingFunc(prog, m, depMap, funcNames)
		case *ssa.Type:
			named, ok := m.Type().(*types.Named)
			if !ok {
				continue
			}
			for method := range named.Methods() {
				if ssaFn := prog.FuncValue(method); ssaFn != nil {
					addMissingFunc(prog, ssaFn, depMap, funcNames)
				}
			}
		}
	}

	return &FunctionDependency{
		PkgPath:    targetPkg.Pkg.Path(),
		DepMap:     depMap,
		FuncNames:  funcNames,
		ChanRanges: chanRanges,
	}, nil
}

func getSSAProgram(pkgcfg *PackageConfig, path string) (*ssa.Program, *ssa.Package, []*packages.Package, error) {
	// Since the results of the `tobari flags` are included in GOFLAGS,
	// this would result in infinite recursion as is.
	// Therefore, GOFLAGS is removed from the environment variables.
	envs := os.Environ()
	newEnvs := make([]string, 0, len(envs))
	for _, kv := range envs {
		i := strings.IndexByte(kv, '=')
		key := kv[:i]
		if key == "GOFLAGS" {
			continue
		}
		newEnvs = append(newEnvs, kv)
	}

	// change working directory to target file path.
	dir := filepath.Dir(path)
	cfg := &packages.Config{
		Mode: packages.LoadAllSyntax,
		Dir:  dir,
		Env:  newEnvs,
	}

	pkgs, err := packages.Load(cfg, ".")
	if err != nil {
		return nil, nil, nil, err
	}
	var pkgErrs []error
	for _, pkg := range pkgs {
		for _, err := range pkg.Errors {
			pkgErrs = append(pkgErrs, err)
		}
	}
	if len(pkgErrs) != 0 {
		return nil, nil, nil, errors.Join(pkgErrs...)
	}

	prog, ssaPkgs := ssautil.AllPackages(pkgs, 0)

	var targetPkg *ssa.Package
	for _, ssaPkg := range ssaPkgs {
		if ssaPkg == nil || ssaPkg.Pkg == nil {
			continue
		}
		if ssaPkg.Pkg.Name() == pkgcfg.PkgName || ssaPkg.Pkg.Path() == pkgcfg.PkgName {
			targetPkg = ssaPkg
			break
		}
	}
	if targetPkg == nil {
		return nil, nil, nil, fmt.Errorf("failed to find target package: %s", pkgcfg.PkgName)
	}

	prog.Build()

	return prog, targetPkg, pkgs, nil
}

func addMissingFunc(prog *ssa.Program, fn *ssa.Function, depMap map[string][]string, funcNames map[funcPos]string) {
	if _, exists := depMap[fn.String()]; exists {
		return
	}
	depMap[fn.String()] = nil
	if fn.Pos().IsValid() {
		pos := prog.Fset.Position(fn.Pos())
		funcNames[funcPos{
			Filename: filepath.Clean(pos.Filename),
			Offset:   pos.Offset,
		}] = fn.String()
	}
	for _, anon := range fn.AnonFuncs {
		if _, exists := depMap[anon.String()]; exists {
			continue
		}
		depMap[anon.String()] = nil
		if anon.Pos().IsValid() {
			pos := prog.Fset.Position(anon.Pos())
			funcNames[funcPos{
				Filename: filepath.Clean(pos.Filename),
				Offset:   pos.Offset,
			}] = anon.String()
		}
	}
}

func analyzeFuncDeps(targetPkg *ssa.Package, n *callgraph.Node) []string {
	depMap := make(map[*ssa.Function]struct{})
	seenMap := make(map[*callgraph.Node]struct{})
	for _, out := range n.Out {
		callee := out.Callee
		if _, exists := seenMap[callee]; exists {
			continue
		}
		seenMap[callee] = struct{}{}
		analyzeFuncDepsRecursive(targetPkg, callee, depMap, seenMap)
	}
	deps := make([]string, 0, len(depMap))
	for dep := range depMap {
		deps = append(deps, dep.String())
	}
	sort.Strings(deps)
	return deps
}

func analyzeFuncDepsRecursive(targetPkg *ssa.Package, n *callgraph.Node, depMap map[*ssa.Function]struct{}, seenMap map[*callgraph.Node]struct{}) {
	fn := n.Func

	if fn != nil && fn.Pkg == targetPkg {
		depMap[fn] = struct{}{}
		return
	}

	path := pkgPath(fn)

	// The runtime package retains references to various anonymous functions and other elements related to GC functionality,
	// which can lead to false positives.
	// Therefore, the search is explicitly terminated.
	if isRuntimePackage(path) {
		return
	}
	// Using references from the net/http package would include all possible candidates that are called back when an HTTP request is received, so they are excluded.
	if isHTTPPackage(path) {
		return
	}
	// Using references from the google.golang.org/grpc package would include all possible candidates that are called back when an gRPC request is received, so they are excluded.
	if isGRPCGoPackage(path) {
		return
	}
	for _, out := range n.Out {
		callee := out.Callee
		if _, exists := seenMap[callee]; exists {
			continue
		}
		seenMap[callee] = struct{}{}
		analyzeFuncDepsRecursive(targetPkg, callee, depMap, seenMap)
	}
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
