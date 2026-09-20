package cover

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"testing"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/tools/go/ssa"
)

// The library the boundary tests use. It is never built: a boundary only
// reads the inside code.
const boundaryTestLib = `package lib

type Handler interface{ Handle(int) int }

type Server struct {
	Handler Handler
	Hook    func(int) int
}

var Default Handler

func Sum(n int) int { return n }
func Apply(n int, fn func(int) int) int { return fn(n) }
func Serve(h Handler) int { return h.Handle(1) }
func Log(v any) {}
func Map[T any](v T, fn func(T) T) T { return fn(v) }
`

const boundaryTestOther = `package other

func Noop() {}
`

// sourceImporter resolves imports from the packages checked so far.
type sourceImporter map[string]*types.Package

func (i sourceImporter) Import(path string) (*types.Package, error) {
	if pkg, ok := i[path]; ok {
		return pkg, nil
	}
	return nil, fmt.Errorf("package %q is not part of the test program", path)
}

// boundariesOf returns the boundaries of the inside package made of appSrc,
// which may import example.com/lib and example.com/other.
func boundariesOf(t *testing.T, appSrc string) boundaries {
	t.Helper()
	fset := token.NewFileSet()
	importer := make(sourceImporter)
	prog := ssa.NewProgram(fset, ssa.InstantiateGenerics)

	var app *ssa.Package
	for _, src := range []struct{ path, code string }{
		{"example.com/lib", boundaryTestLib},
		{"example.com/other", boundaryTestOther},
		{"example.com/app", appSrc},
	} {
		file, err := parser.ParseFile(fset, src.path+"/file.go", src.code, 0)
		if err != nil {
			t.Fatal(err)
		}
		info := &types.Info{
			Types:        make(map[ast.Expr]types.TypeAndValue),
			Defs:         make(map[*ast.Ident]types.Object),
			Uses:         make(map[*ast.Ident]types.Object),
			Implicits:    make(map[ast.Node]types.Object),
			Instances:    make(map[*ast.Ident]types.Instance),
			Scopes:       make(map[ast.Node]*types.Scope),
			Selections:   make(map[*ast.SelectorExpr]*types.Selection),
			FileVersions: make(map[*ast.File]string),
		}
		pkg, err := (&types.Config{Importer: importer}).Check(src.path, fset, []*ast.File{file}, info)
		if err != nil {
			t.Fatal(err)
		}
		importer[src.path] = pkg
		app = prog.CreatePackage(pkg, []*ast.File{file}, info, false)
	}
	app.Build()
	return computeBoundaries(prog, []*ssa.Package{app})
}

const boundaryTestBase = `package app

import "example.com/lib"

type handler struct{ n int }

func (h handler) Handle(v int) int { return v + h.n }

type other struct{}

func (other) Handle(v int) int { return v }

func double(v int) int { return v * 2 }
func triple(v int) int { return v * 3 }

func Compute(n int) int {
	total := lib.Sum(n)
	total += lib.Serve(handler{n: 1})
	return total
}
`

func TestBoundaries(t *testing.T) {
	base := boundariesOf(t, boundaryTestBase)
	if len(base) != 1 || base["example.com/lib"] == "" {
		t.Fatalf("expected a boundary for lib only, got %v", base)
	}

	const compute = `func Compute(n int) int {
	total := lib.Sum(n)
	total += lib.Serve(handler{n: 1})
	return total
}
`
	replaceCompute := func(replacement string) string {
		return boundaryTestBase[:len(boundaryTestBase)-len(compute)] + replacement
	}

	tests := []struct {
		name string
		src  string
		// changed lists the packages whose boundary must differ from base.
		changed []string
	}{
		{
			name: "editing a function body around the uses changes nothing",
			src: replaceCompute(`func Compute(n int) int {
	if n < 0 {
		n = -n
	}
	total := lib.Sum(n + 1)
	total += 2 * lib.Serve(handler{n: 7})
	return total - 1
}
`),
		},
		{
			name: "adding code that uses no outside package changes nothing",
			src: boundaryTestBase + `
func Extra(n int) int { return double(triple(n)) }
`,
		},
		{
			name: "repeating a use within the same function changes nothing",
			src: replaceCompute(`func Compute(n int) int {
	total := lib.Sum(n) + lib.Sum(n+1)
	total += lib.Serve(handler{n: 1})
	return total
}
`),
		},
		{
			name: "calling another function of the package",
			src: replaceCompute(`func Compute(n int) int {
	total := lib.Sum(n)
	total += lib.Serve(handler{n: 1})
	return lib.Apply(total, double)
}
`),
			changed: []string{"example.com/lib"},
		},
		{
			name: "handing over a value of another type",
			src: replaceCompute(`func Compute(n int) int {
	total := lib.Sum(n)
	total += lib.Serve(other{})
	return total
}
`),
			changed: []string{"example.com/lib"},
		},
		{
			name: "using the package from another function",
			src: boundaryTestBase + `
func Extra(n int) int { return lib.Sum(n) }
`,
			changed: []string{"example.com/lib"},
		},
		{
			name: "moving a use into a function literal",
			src: replaceCompute(`func Compute(n int) int {
	total := func() int { return lib.Sum(n) }()
	total += lib.Serve(handler{n: 1})
	return total
}
`),
			changed: []string{"example.com/lib"},
		},
		{
			name: "storing a callback into a struct field",
			src: replaceCompute(`func Compute(n int) int {
	total := lib.Sum(n)
	total += lib.Serve(handler{n: 1})
	srv := &lib.Server{Hook: double}
	_ = srv
	return total
}
`),
			changed: []string{"example.com/lib"},
		},
		{
			name: "storing into a package variable",
			src: replaceCompute(`func Compute(n int) int {
	total := lib.Sum(n)
	total += lib.Serve(handler{n: 1})
	lib.Default = handler{n: 2}
	return total
}
`),
			changed: []string{"example.com/lib"},
		},
		{
			name: "converting to an interface of the package without calling it",
			src: replaceCompute(`func Compute(n int) int {
	total := lib.Sum(n)
	total += lib.Serve(handler{n: 1})
	var h lib.Handler = other{}
	_ = h
	return total
}
`),
			changed: []string{"example.com/lib"},
		},
		{
			name: "handing a value over as an empty interface",
			src: replaceCompute(`func Compute(n int) int {
	total := lib.Sum(n)
	total += lib.Serve(handler{n: 1})
	lib.Log(handler{n: 3})
	return total
}
`),
			changed: []string{"example.com/lib"},
		},
		{
			name: "instantiating a generic function",
			src: replaceCompute(`func Compute(n int) int {
	total := lib.Sum(n)
	total += lib.Serve(handler{n: 1})
	return lib.Map(total, double)
}
`),
			changed: []string{"example.com/lib"},
		},
		{
			name: "starting to use another package leaves the first one alone",
			src: `package app

import (
	"example.com/lib"
	"example.com/other"
)

type handler struct{ n int }

func (h handler) Handle(v int) int { return v + h.n }

func double(v int) int { return v * 2 }
func triple(v int) int { return v * 3 }

func Compute(n int) int {
	total := lib.Sum(n)
	total += lib.Serve(handler{n: 1})
	other.Noop()
	return total
}
`,
			changed: []string{"example.com/other"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := boundariesOf(t, test.src).changedFrom(base)
			if diff := cmp.Diff(test.changed, got); diff != "" {
				t.Errorf("changed boundaries (-want +got):\n%s", diff)
			}
		})
	}
}

// TestBoundaryUseKinds pins that the different ways of handing a value over
// are told apart: two programs that differ only in the callback they hand
// over, or only in the way they do it, must not share a boundary.
func TestBoundaryUseKinds(t *testing.T) {
	program := func(body string) string {
		return `package app

import "example.com/lib"

func double(v int) int { return v * 2 }
func triple(v int) int { return v * 3 }

func Compute(n int) int {
` + body + `
}
`
	}
	bodies := map[string]string{
		"apply double": "return lib.Apply(n, double)",
		// The function handed over is a reference of its own.
		"apply triple":   "return lib.Apply(n, triple)",
		"field double":   "srv := &lib.Server{Hook: double}; _ = srv; return n",
		"literal":        "return lib.Apply(n, func(v int) int { return v })",
		"generic int":    "return lib.Map(n, double)",
		"generic string": `return len(lib.Map("a", func(s string) string { return s }))`,
	}
	seen := make(map[string]string)
	for name, body := range bodies {
		fingerprint := boundariesOf(t, program(body))["example.com/lib"]
		if fingerprint == "" {
			t.Fatalf("%s: no boundary for lib", name)
		}
		if previous, ok := seen[fingerprint]; ok {
			t.Errorf("%q and %q share a boundary", previous, name)
		}
		seen[fingerprint] = name
	}
}
