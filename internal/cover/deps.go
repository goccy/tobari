package cover

import (
	"errors"
	"fmt"
	"go/ast"
	"go/types"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/packages"
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
