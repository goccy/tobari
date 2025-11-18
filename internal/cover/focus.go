package cover

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"golang.org/x/tools/go/callgraph"
	"golang.org/x/tools/go/callgraph/cha"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

func focus(pkg, path string) error {
	if _, err := run(pkg, path); err != nil {
		return err
	}
	return nil
}

func run(pkg, path string) (any, error) {
	prog, ssaPkgs, targetPkg, err := loadCompletePackageGraph(pkg, path)
	if err != nil {
		return nil, err
	}
	_ = ssaPkgs

	prog.Build()

	cg := cha.CallGraph(prog)
	for _, n := range cg.Nodes {
		if n.Func == nil || n.Func.Pkg != targetPkg {
			continue
		}
		depMap := make(map[*ssa.Function]struct{})
		analyze(targetPkg, n, depMap, make(map[*callgraph.Node]struct{}))
		delete(depMap, n.Func)
		deps := make([]string, 0, len(depMap))
		for dep := range depMap {
			deps = append(deps, dep.String())
		}
		sort.Strings(deps)
		fmt.Fprintf(os.Stderr, "func: %s\ndeps: %v\n\n", n.Func.String(), deps)
	}
	return nil, nil
}

func analyze(targetPkg *ssa.Package, n *callgraph.Node, depMap map[*ssa.Function]struct{}, seenMap map[*callgraph.Node]struct{}) {
	fn := n.Func
	if fn != nil && fn.Pkg == targetPkg {
		depMap[fn] = struct{}{}
	}
	for _, out := range n.Out {
		callee := out.Callee
		if _, exists := seenMap[callee]; exists {
			continue
		}
		seenMap[callee] = struct{}{}
		analyze(targetPkg, callee, depMap, seenMap)
	}
}

func loadCompletePackageGraph(pkg, path string) (*ssa.Program, []*ssa.Package, *ssa.Package, error) {
	dir := filepath.Dir(path)

	// Configure packages.Load to get complete dependency information
	cfg := &packages.Config{
		Mode: packages.LoadAllSyntax,
		Dir:  dir,
		Env: []string{
			fmt.Sprintf("GOCACHE=%s", os.Getenv("GOCACHE")),
		},
	}

	// Load the target package and all its dependencies
	start := time.Now()
	pkgs, err := packages.Load(cfg, ".")
	if err != nil {
		return nil, nil, nil, err
	}
	fmt.Fprintf(os.Stderr, "load: elapsed time: %v\n", time.Since(start))

	if packages.PrintErrors(pkgs) > 0 {
		return nil, nil, nil, err
	}

	// Create SSA program from all loaded packages
	prog, ssaPkgs := ssautil.AllPackages(pkgs, 0)

	// Find the target package by name
	var targetPkg *ssa.Package
	for _, ssaPkg := range ssaPkgs {
		if ssaPkg.Pkg.Name() == pkg || ssaPkg.Pkg.Path() == pkg {
			targetPkg = ssaPkg
			break
		}
	}

	if targetPkg == nil {
		return nil, nil, nil, fmt.Errorf("failed to find target package: %s", pkg)
	}

	return prog, ssaPkgs, targetPkg, nil
}
