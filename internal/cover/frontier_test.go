package cover

import (
	"fmt"
	"go/token"
	"go/types"
	"math/rand"
	"reflect"
	"sort"
	"testing"

	"golang.org/x/tools/go/callgraph"
	"golang.org/x/tools/go/ssa"
)

// The tests below build call graphs out of bare *ssa.Function values: the
// dependency analysis only reads a function's package (and its Origin, which
// is nil for a non-instantiated function), so a function needs nothing more
// than a package and a signature for String() to work. Each function gets its
// own package so that names stay distinct.

const (
	testCoverPkgPrefix = "example.com/app/cover"
	testOtherPkgPrefix = "example.com/dep/other"
)

func newTestFunc(pkgPath string) *ssa.Function {
	return &ssa.Function{
		Signature: types.NewSignatureType(nil, nil, nil, nil, nil, false),
		Pkg:       &ssa.Package{Pkg: types.NewPackage(pkgPath, "p")},
	}
}

// testGraph is a call graph built from a compact description of its nodes.
type testGraph struct {
	graph       *callgraph.Graph
	nodes       map[string]*callgraph.Node
	coverPkgSet map[string]struct{}
}

// newTestGraph creates one node per key of edges. A key starting with "cover"
// is a coverage-target function; "runtime", "http" and "grpc" prefixes place
// the function in the corresponding cut-off package; anything else is an
// ordinary dependency function. Edges carry no call site, so every edge is
// followable.
func newTestGraph(edges map[string][]string) *testGraph {
	names := make([]string, 0, len(edges))
	for name := range edges {
		names = append(names, name)
	}
	sort.Strings(names)

	tg := &testGraph{
		nodes:       make(map[string]*callgraph.Node, len(names)),
		coverPkgSet: make(map[string]struct{}),
	}
	fns := make(map[string]*ssa.Function, len(names))
	for _, name := range names {
		var pkgPath string
		switch {
		case hasPrefix(name, "cover"):
			pkgPath = testCoverPkgPrefix + "/" + name
			tg.coverPkgSet[pkgPath] = struct{}{}
		case hasPrefix(name, "runtime"):
			pkgPath = "runtime/" + name
		case hasPrefix(name, "http"):
			pkgPath = "net/http"
		case hasPrefix(name, "grpc"):
			pkgPath = "google.golang.org/grpc/" + name
		default:
			pkgPath = testOtherPkgPrefix + "/" + name
		}
		fns[name] = newTestFunc(pkgPath)
	}
	tg.graph = callgraph.New(fns[names[0]])
	for _, name := range names {
		tg.nodes[name] = tg.graph.CreateNode(fns[name])
	}
	for _, name := range names {
		for _, callee := range edges[name] {
			callgraph.AddEdge(tg.nodes[name], nil, tg.nodes[callee])
		}
	}
	return tg
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func (tg *testGraph) coverName(name string) string {
	return normalizeFuncName(tg.nodes[name].Func)
}

// emptyVTAGraph stands in for a VTA graph when every edge is static (or, as in
// these tests, has no site) and needs no confirmation.
func emptyVTAGraph() *callgraph.Graph {
	return &callgraph.Graph{Nodes: make(map[*ssa.Function]*callgraph.Node)}
}

// referenceDeps is the per-root walk the frontier computation replaces: it
// explores from n's followable callees, records each coverage target it
// reaches first and stops there, and is cut off at the excluded packages.
// Kept as the executable definition of what a frontier is.
func referenceDeps(coverPkgSet map[string]struct{}, n *callgraph.Node, followable *followableCallees) []string {
	depMap := make(map[string]struct{})
	seen := make(map[*callgraph.Node]struct{})
	var walk func(*callgraph.Node)
	walk = func(n *callgraph.Node) {
		fn := n.Func
		if fn != nil && funcCoverPkgPath(fn, coverPkgSet) != "" {
			depMap[normalizeFuncName(fn)] = struct{}{}
			return
		}
		path := resolvePkgPath(fn)
		if isRuntimePackage(path) || isHTTPPackage(path) || isGRPCGoPackage(path) {
			return
		}
		for _, callee := range followable.from(n) {
			if _, ok := seen[callee]; ok {
				continue
			}
			seen[callee] = struct{}{}
			walk(callee)
		}
	}
	for _, callee := range followable.from(n) {
		if _, ok := seen[callee]; ok {
			continue
		}
		seen[callee] = struct{}{}
		walk(callee)
	}
	deps := make([]string, 0, len(depMap))
	for dep := range depMap {
		deps = append(deps, dep)
	}
	sort.Strings(deps)
	return deps
}

func TestFrontiersDeps(t *testing.T) {
	tests := []struct {
		name  string
		edges map[string][]string
		want  map[string][]string // root -> expected cover deps (by node name)
	}{
		{
			// A ⇄ B, and only A also reaches the target. Caching on first visit
			// would settle B while A is still open and record nothing for B.
			name: "cycle where one member alone reaches the target",
			edges: map[string][]string{
				"coverRoot": {"a"},
				"a":         {"b", "coverY"},
				"b":         {"a"},
				"coverY":    {},
			},
			want: map[string][]string{
				"coverRoot": {"coverY"},
				"a":         {"coverY"},
				"b":         {"coverY"},
			},
		},
		{
			// The root is a coverage target inside its own cycle: it appears
			// in its own dependency list, as the walk stops at it.
			name: "root reached through its own callees",
			edges: map[string][]string{
				"coverRoot": {"a"},
				"a":         {"coverRoot", "coverZ"},
				"coverZ":    {},
			},
			want: map[string][]string{
				"coverRoot": {"coverRoot", "coverZ"},
				"a":         {"coverRoot", "coverZ"},
			},
		},
		{
			// A coverage target is the boundary: what lies beyond it belongs to
			// its own frontier, not to its callers'.
			name: "walk stops at a coverage target",
			edges: map[string][]string{
				"coverRoot": {"coverMid"},
				"coverMid":  {"coverFar"},
				"coverFar":  {},
			},
			want: map[string][]string{
				"coverRoot": {"coverMid"},
				"coverMid":  {"coverFar"},
				"coverFar":  {},
			},
		},
		{
			// Cut-off packages contribute nothing and hide what they call, but
			// a root in such a package still has its own callees expanded.
			name: "cut-off packages",
			edges: map[string][]string{
				"coverRoot":   {"runtimeA", "httpA", "grpcA", "b"},
				"runtimeA":    {"coverHidden"},
				"httpA":       {"coverHidden"},
				"grpcA":       {"coverHidden"},
				"b":           {"coverSeen"},
				"coverHidden": {},
				"coverSeen":   {},
			},
			want: map[string][]string{
				"coverRoot": {"coverSeen"},
				"runtimeA":  {"coverHidden"},
			},
		},
		{
			// Two cycles chained: the inner group's frontier is decided before
			// the outer group's, and both see the target.
			name: "nested cycles",
			edges: map[string][]string{
				"coverRoot": {"a"},
				"a":         {"b"},
				"b":         {"a", "c"},
				"c":         {"d"},
				"d":         {"c", "coverT"},
				"coverT":    {},
			},
			want: map[string][]string{
				"coverRoot": {"coverT"},
				"a":         {"coverT"},
				"c":         {"coverT"},
			},
		},
		{
			name: "self loop with no target",
			edges: map[string][]string{
				"coverRoot": {"a"},
				"a":         {"a"},
			},
			want: map[string][]string{
				"coverRoot": {},
				"a":         {},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tg := newTestGraph(tt.edges)
			followable := newFollowableCallees(tg.graph, emptyVTAGraph())
			fr := newFrontiers(tg.graph, followable, tg.coverPkgSet)
			for root, wantNames := range tt.want {
				want := make([]string, 0, len(wantNames))
				for _, name := range wantNames {
					want = append(want, tg.coverName(name))
				}
				sort.Strings(want)
				got := fr.depsFrom(tg.nodes[root], followable)
				if got == nil {
					t.Fatalf("%s: depsFrom returned nil; want a non-nil slice", root)
				}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("%s: deps = %v, want %v", root, got, want)
				}
			}
		})
	}
}

// TestFrontiersMatchReferenceWalk checks the shared, group-based computation
// against the per-root walk on random graphs dense in cycles.
func TestFrontiersMatchReferenceWalk(t *testing.T) {
	for seed := int64(0); seed < 500; seed++ {
		r := rand.New(rand.NewSource(seed))
		numNodes := 1 + r.Intn(40)
		edges := make(map[string][]string, numNodes)
		names := make([]string, numNodes)
		for i := range names {
			var kind string
			switch r.Intn(10) {
			case 0, 1, 2:
				kind = "cover"
			case 3:
				kind = "runtime"
			case 4:
				kind = "http"
			case 5:
				kind = "grpc"
			default:
				kind = "fn"
			}
			names[i] = fmt.Sprintf("%s%d", kind, i)
		}
		density := r.Intn(12) + 1
		for _, name := range names {
			for j, k := 0, r.Intn(density); j < k; j++ {
				edges[name] = append(edges[name], names[r.Intn(numNodes)])
			}
			if edges[name] == nil {
				edges[name] = []string{}
			}
		}

		tg := newTestGraph(edges)
		followable := newFollowableCallees(tg.graph, emptyVTAGraph())
		fr := newFrontiers(tg.graph, followable, tg.coverPkgSet)
		for name, n := range tg.nodes {
			want := referenceDeps(tg.coverPkgSet, n, followable)
			got := fr.depsFrom(n, followable)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("seed %d, node %s: deps = %v, want %v", seed, name, got, want)
			}
		}
	}
}

// followEdgeLinear is the edge policy stated independently of the lookup
// structure the code under test builds: it scans the VTA out-edges of the
// edge's own caller. Used as the oracle for per-caller confirmation.
func followEdgeLinear(vtaGraph *callgraph.Graph, e *callgraph.Edge) bool {
	if followedWithoutVTA(e.Site, e.Callee.Func) {
		return true
	}
	caller := vtaGraph.Nodes[e.Caller.Func]
	if caller == nil {
		return false
	}
	for _, ve := range caller.Out {
		if ve.Site == e.Site && ve.Callee.Func == e.Callee.Func {
			return true
		}
	}
	return false
}

// TestFollowableCalleesMatchesEdgePolicy builds random graphs whose call sites
// are owned by their caller, the way rta and vta both record them, and checks
// the per-caller confirmation against a linear scan of the caller's VTA
// out-edges. The frontier results are checked against the reference walk on
// the same graphs, so the dynamic-call path is covered end to end.
func TestFollowableCalleesMatchesEdgePolicy(t *testing.T) {
	for seed := int64(0); seed < 300; seed++ {
		r := rand.New(rand.NewSource(seed))
		numNodes := 1 + r.Intn(25)

		fns := make([]*ssa.Function, numNodes)
		coverPkgSet := make(map[string]struct{})
		for i := range fns {
			var pkgPath string
			switch r.Intn(8) {
			case 0, 1:
				pkgPath = fmt.Sprintf("%s/c%d", testCoverPkgPrefix, i)
				coverPkgSet[pkgPath] = struct{}{}
			case 2:
				pkgPath = fmt.Sprintf("runtime/r%d", i)
			case 3:
				pkgPath = "net/http"
			case 4:
				pkgPath = fmt.Sprintf("google.golang.org/grpc/g%d", i)
			default:
				pkgPath = fmt.Sprintf("%s/d%d", testOtherPkgPrefix, i)
			}
			fns[i] = newTestFunc(pkgPath)
		}

		rtaGraph := callgraph.New(fns[0])
		nodes := make([]*callgraph.Node, numNodes)
		for i, fn := range fns {
			nodes[i] = rtaGraph.CreateNode(fn)
		}

		// Sites are pooled per caller and shared between that caller's edges,
		// the way RTA fans one dynamic call site out to many callees.
		type callPair struct {
			site   ssa.CallInstruction
			callee *ssa.Function
		}
		pairs := make(map[int][]callPair, numNodes)
		for i := range nodes {
			pool := make([]ssa.CallInstruction, 0, 3)
			for p, n := 0, 1+r.Intn(3); p < n; p++ {
				pool = append(pool, randomSite(r, fns[r.Intn(numNodes)]))
			}
			for e, n := 0, r.Intn(7); e < n; e++ {
				site := pool[r.Intn(len(pool))]
				callee := fns[r.Intn(numNodes)]
				callgraph.AddEdge(nodes[i], site, rtaGraph.Nodes[callee])
				pairs[i] = append(pairs[i], callPair{site: site, callee: callee})
			}
		}

		// A VTA graph recorded the way vta does: every edge of a caller carries
		// a site belonging to that same caller. Some callers are absent, some
		// pairs are unconfirmed, and some confirmations are for pairs RTA never
		// proposes.
		vtaGraph := emptyVTAGraph()
		for i := range nodes {
			if r.Intn(5) == 0 {
				continue
			}
			vtaCaller := vtaGraph.CreateNode(fns[i])
			for _, p := range pairs[i] {
				if r.Intn(2) == 0 {
					continue
				}
				callgraph.AddEdge(vtaCaller, p.site, vtaGraph.CreateNode(p.callee))
			}
			if len(pairs[i]) > 0 && r.Intn(3) == 0 {
				callgraph.AddEdge(vtaCaller, pairs[i][0].site, vtaGraph.CreateNode(fns[r.Intn(numNodes)]))
			}
		}

		followable := newFollowableCallees(rtaGraph, vtaGraph)
		fr := newFrontiers(rtaGraph, followable, coverPkgSet)

		for i, n := range nodes {
			var want []*callgraph.Node
			kept := make(map[*callgraph.Node]bool, len(n.Out))
			for _, e := range n.Out {
				if !followEdgeLinear(vtaGraph, e) || kept[e.Callee] {
					continue
				}
				kept[e.Callee] = true
				want = append(want, e.Callee)
			}
			if got := followable.from(n); !reflect.DeepEqual(got, want) {
				t.Fatalf("seed %d, node %d: followable callees = %v, want %v", seed, i, got, want)
			}
			wantDeps := referenceDeps(coverPkgSet, n, followable)
			if got := fr.depsFrom(n, followable); !reflect.DeepEqual(got, wantDeps) {
				t.Fatalf("seed %d, node %d: deps = %v, want %v", seed, i, got, wantDeps)
			}
		}
	}
}

func TestFollowableCalleesDedupesParallelEdges(t *testing.T) {
	tg := newTestGraph(map[string][]string{
		"coverRoot": {"a", "a", "b", "a"},
		"a":         {},
		"b":         {},
	})
	followable := newFollowableCallees(tg.graph, emptyVTAGraph())
	got := followable.from(tg.nodes["coverRoot"])
	if len(got) != 2 {
		t.Fatalf("from(coverRoot) = %d callees, want 2 distinct", len(got))
	}
	if got[0] != tg.nodes["a"] || got[1] != tg.nodes["b"] {
		t.Errorf("from(coverRoot) = %v, want [a b] in first-seen order", got)
	}
	if got := followable.from(tg.nodes["a"]); got != nil {
		t.Errorf("from(a) = %v, want nil for a node without out-edges", got)
	}
}

// dynamicSite is a call site whose callee is not statically known, so the
// edge policy asks the VTA graph about it.
func dynamicSite() ssa.CallInstruction {
	return &ssa.Call{Call: ssa.CallCommon{Value: &ssa.Parameter{}}}
}

// staticSite is a call site that names its callee directly.
func staticSite(callee *ssa.Function) ssa.CallInstruction {
	return &ssa.Call{Call: ssa.CallCommon{Value: callee}}
}

// invokeSite is an interface method call. Its callee here is never an
// instantiated generic, so the policy asks the VTA graph about it.
func invokeSite() ssa.CallInstruction {
	sig := types.NewSignatureType(nil, nil, nil, nil, nil, false)
	method := types.NewFunc(token.NoPos, types.NewPackage("example.com/iface", "iface"), "M", sig)
	return &ssa.Call{Call: ssa.CallCommon{Value: &ssa.Parameter{}, Method: method}}
}

// randomSite picks one of the call-site shapes the policy distinguishes.
func randomSite(r *rand.Rand, staticCallee *ssa.Function) ssa.CallInstruction {
	switch r.Intn(4) {
	case 0:
		return nil // a reflect edge, which RTA records without a site
	case 1:
		return staticSite(staticCallee)
	case 2:
		return dynamicSite()
	default:
		return invokeSite()
	}
}

func TestFollowableCalleesConfirmsDynamicEdgesPerCaller(t *testing.T) {
	caller := newTestFunc(testCoverPkgPrefix + "/caller")
	other := newTestFunc(testCoverPkgPrefix + "/other")
	static := newTestFunc(testOtherPkgPrefix + "/static")
	confirmed := newTestFunc(testOtherPkgPrefix + "/confirmed")
	rejected := newTestFunc(testOtherPkgPrefix + "/rejected")
	reflected := newTestFunc(testOtherPkgPrefix + "/reflected")

	site := dynamicSite()

	rta := callgraph.New(caller)
	callerNode := rta.CreateNode(caller)
	otherNode := rta.CreateNode(other)
	callgraph.AddEdge(callerNode, staticSite(static), rta.CreateNode(static))
	callgraph.AddEdge(callerNode, site, rta.CreateNode(confirmed))
	callgraph.AddEdge(callerNode, site, rta.CreateNode(rejected))
	callgraph.AddEdge(callerNode, nil, rta.CreateNode(reflected))
	// other makes the same kind of dynamic call, but has no VTA node, so
	// nothing confirms its edge.
	callgraph.AddEdge(otherNode, dynamicSite(), rta.Nodes[confirmed])

	vta := emptyVTAGraph()
	vtaCaller := vta.CreateNode(caller)
	callgraph.AddEdge(vtaCaller, site, vta.CreateNode(confirmed))

	followable := newFollowableCallees(rta, vta)

	got := followable.from(callerNode)
	want := []*callgraph.Node{rta.Nodes[static], rta.Nodes[confirmed], rta.Nodes[reflected]}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("from(caller) = %v, want static, confirmed and site-less edges kept and the unconfirmed one dropped", got)
	}
	if got := followable.from(otherNode); got != nil {
		t.Errorf("from(other) = %v, want nil: no VTA node for the caller", got)
	}
}
