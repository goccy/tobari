package cover

import (
	"fmt"
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

type FunctionDependency struct {
	PkgPath string
	DepMap  map[string][]string
}

func createFunctionDependencyMap(pkg, path string) (*FunctionDependency, error) {
	prog, targetPkg, err := getSSAProgram(pkg, path)
	if err != nil {
		return nil, err
	}

	graph := vta.CallGraph(ssautil.AllFunctions(prog), cha.CallGraph(prog))
	depMap := make(map[string][]string)
	for _, n := range graph.Nodes {
		if n.Func == nil || n.Func.Pkg != targetPkg {
			continue
		}
		depMap[n.Func.String()] = analyzeFuncDeps(targetPkg, n)
	}
	return &FunctionDependency{
		PkgPath: targetPkg.Pkg.Path(),
		DepMap:  depMap,
	}, nil
}

func getSSAProgram(pkg, path string) (*ssa.Program, *ssa.Package, error) {
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
		return nil, nil, err
	}

	prog, ssaPkgs := ssautil.AllPackages(pkgs, 0)

	var targetPkg *ssa.Package
	for _, ssaPkg := range ssaPkgs {
		if ssaPkg == nil || ssaPkg.Pkg == nil {
			continue
		}
		if ssaPkg.Pkg.Name() == pkg || ssaPkg.Pkg.Path() == pkg {
			targetPkg = ssaPkg
			break
		}
	}
	if targetPkg == nil {
		return nil, nil, fmt.Errorf("failed to find target package: %s", pkg)
	}

	prog.Build()

	return prog, targetPkg, nil
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
