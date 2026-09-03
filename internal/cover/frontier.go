package cover

import (
	"math/bits"
	"sort"

	"golang.org/x/tools/go/callgraph"
)

// Frontiers: each node's dependency set, decided once.
//
// A node's frontier is the set of coverage targets a walk from it reaches
// first — the boundary where the walk stops. The walk stops at a coverage
// target and is cut off at runtime, net/http and grpc-go; targets beyond the
// boundary are not part of it, they sit on the frontiers of those targets.
// CreateMainDeps needs, for every coverage-target function, the frontier of
// its callees.
//
// All three stop conditions read the node alone, so a frontier is a property
// of the node: it can be decided once and shared, instead of re-walking the
// graph from every coverage-target function.
//
// # Why the graph is grouped before deciding
//
// Deciding on first visit and caching the frontier is wrong on its own. A node
// whose expansion re-enters a node that is still being decided would have to
// treat that node as contributing nothing, and caching the result would hand
// every later reader the same omission.
//
// Groups of nodes that can all reach each other avoid it. Coverage targets and
// cut-off packages are handed to the grouping as if they had no out-edges, so
// they never land inside such a group. That leaves every group made of nodes the
// walk does expand, and because each member reaches every other member, they all
// reach the same things outside the group — one frontier serves the whole group.
// Groups come out with every group they point at already decided, so one pass
// settles them (this is the strongly connected components of the graph, in
// reverse topological order).
//
// # Why frontiers are bit sets
//
// A service has few distinct coverage targets (a few thousand) next to the
// number of graph nodes, so a frontier fits in numTargets/8 bytes whatever its
// size, and a union is a word loop. Holding []string per node instead would
// cost more in slice headers alone than the rest of the analysis uses.

// coverBitIndex assigns a bit position to every coverage-target function name.
//
// Positions follow sorted name order, so reading a frontier from low bit to
// high yields the names already sorted, the order the suppDeps entries carry.
type coverBitIndex struct {
	bitOf  map[string]int
	nameOf []string
	words  int
}

func newCoverBitIndex(rtaGraph *callgraph.Graph, coverPkgSet map[string]struct{}) *coverBitIndex {
	seen := make(map[string]struct{})
	for fn := range rtaGraph.Nodes {
		if fn == nil || funcCoverPkgPath(fn, coverPkgSet) == "" {
			continue
		}
		seen[normalizeFuncName(fn)] = struct{}{}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)

	idx := &coverBitIndex{
		bitOf:  make(map[string]int, len(names)),
		nameOf: names,
		words:  (len(names) + 63) / 64,
	}
	for i, name := range names {
		idx.bitOf[name] = i
	}
	return idx
}

// decode lists the names in a frontier, sorted.
//
// The result is non-nil even when empty: the suppDeps JSON writes [] rather
// than null for a function whose frontier is empty.
func (ci *coverBitIndex) decode(set []uint64) []string {
	out := make([]string, 0, 8)
	for w, word := range set {
		for word != 0 {
			out = append(out, ci.nameOf[w*64+bits.TrailingZeros64(word)])
			word &= word - 1 // clear the lowest set bit
		}
	}
	return out
}

// frontierSet is one frontier, shared by every node of the same group. Sets
// are compared by pointer so that a group reached through a single other set
// can point at it instead of copying.
type frontierSet struct {
	bits []uint64
}

// frontiers holds every node's frontier.
type frontiers struct {
	ci     *coverBitIndex
	byNode map[*callgraph.Node]*frontierSet
	empty  *frontierSet
}

// notExpanded reports the frontier of a node the walk stops at: a coverage
// target contributes itself, and runtime, net/http and grpc-go are cut off.
func (fr *frontiers) notExpanded(n *callgraph.Node, coverPkgSet map[string]struct{}) (*frontierSet, bool) {
	fn := n.Func
	if fn != nil && funcCoverPkgPath(fn, coverPkgSet) != "" {
		bit := fr.ci.bitOf[normalizeFuncName(fn)]
		set := make([]uint64, fr.ci.words)
		set[bit/64] |= 1 << uint(bit%64)
		return &frontierSet{bits: set}, true
	}
	path := resolvePkgPath(fn)
	if isRuntimePackage(path) || isHTTPPackage(path) || isGRPCGoPackage(path) {
		return fr.empty, true
	}
	return nil, false
}

func newFrontiers(rtaGraph *callgraph.Graph, followable *followableEdges, coverPkgSet map[string]struct{}) *frontiers {
	fr := &frontiers{
		ci:     newCoverBitIndex(rtaGraph, coverPkgSet),
		byNode: make(map[*callgraph.Node]*frontierSet, len(rtaGraph.Nodes)),
		empty:  &frontierSet{},
	}

	// A node the walk stops at contributes its own frontier and is never
	// expanded, so the grouping below must not see its out-edges either.
	stops := make(map[*callgraph.Node]*frontierSet, len(rtaGraph.Nodes))
	for _, n := range rtaGraph.Nodes {
		if set, ok := fr.notExpanded(n, coverPkgSet); ok {
			stops[n] = set
		}
	}
	edgesOf := func(n *callgraph.Node) []*callgraph.Edge {
		if _, ok := stops[n]; ok {
			return nil
		}
		return followable.from(n)
	}

	// Group the nodes that can all reach each other, iteratively so that a deep
	// graph cannot exhaust the stack. Each group is completed only after every
	// group it points at, so its frontier can be assembled on the spot.
	type frame struct {
		node  *callgraph.Node
		edges []*callgraph.Edge
		next  int
	}
	var (
		order     = make(map[*callgraph.Node]int, len(rtaGraph.Nodes))
		low       = make(map[*callgraph.Node]int, len(rtaGraph.Nodes))
		open      = make(map[*callgraph.Node]bool, len(rtaGraph.Nodes))
		groupOf   = make(map[*callgraph.Node]int, len(rtaGraph.Nodes))
		pending   []*callgraph.Node
		frames    []*frame
		counter   int
		nextGroup int
	)

	begin := func(n *callgraph.Node) {
		order[n] = counter
		low[n] = counter
		counter++
		pending = append(pending, n)
		open[n] = true
		frames = append(frames, &frame{node: n, edges: edgesOf(n)})
	}

	// settle assigns one frontier to every member of a completed group: the
	// union of the frontiers of everything the group points at from outside
	// itself.
	settle := func(members []*callgraph.Node) {
		id := nextGroup
		nextGroup++
		// Numbered first so that the loops below can tell an edge that leaves
		// the group from one that stays inside it.
		for _, m := range members {
			groupOf[m] = id
		}

		contributions := func(yield func(*frontierSet)) {
			for _, m := range members {
				if set, ok := stops[m]; ok {
					yield(set)
					continue
				}
				for _, e := range followable.from(m) {
					if groupOf[e.Callee] == id {
						continue // stays inside; adds nothing to what we are building
					}
					if set := fr.byNode[e.Callee]; set != nil {
						yield(set)
					}
				}
			}
		}

		// One distinct contribution can be pointed at rather than copied, which
		// is what keeps a chain of interior nodes from allocating per node.
		var (
			only     *frontierSet
			distinct int
		)
		contributions(func(set *frontierSet) {
			if set == fr.empty {
				return
			}
			switch {
			case only == nil:
				only, distinct = set, 1
			case only != set:
				distinct = 2
			}
		})

		frontier := fr.empty
		switch distinct {
		case 0:
		case 1:
			frontier = only
		default:
			union := make([]uint64, fr.ci.words)
			contributions(func(set *frontierSet) {
				for i := range set.bits {
					union[i] |= set.bits[i]
				}
			})
			frontier = &frontierSet{bits: union}
		}
		for _, m := range members {
			fr.byNode[m] = frontier
		}
	}

	for _, root := range rtaGraph.Nodes {
		if _, seen := order[root]; seen {
			continue
		}
		begin(root)
		for len(frames) > 0 {
			f := frames[len(frames)-1]
			if f.next < len(f.edges) {
				w := f.edges[f.next].Callee
				f.next++
				if _, seen := order[w]; !seen {
					begin(w)
				} else if open[w] && order[w] < low[f.node] {
					low[f.node] = order[w]
				}
				continue
			}
			frames = frames[:len(frames)-1]
			if low[f.node] == order[f.node] {
				var members []*callgraph.Node
				for {
					m := pending[len(pending)-1]
					pending = pending[:len(pending)-1]
					open[m] = false
					members = append(members, m)
					if m == f.node {
						break
					}
				}
				settle(members)
			}
			if len(frames) > 0 {
				parent := frames[len(frames)-1]
				if low[f.node] < low[parent.node] {
					low[parent.node] = low[f.node]
				}
			}
		}
	}

	return fr
}

// depsFrom returns the frontier of n's followable callees, sorted — the
// per-function value stored in the suppDeps map.
func (fr *frontiers) depsFrom(n *callgraph.Node, followable *followableEdges) []string {
	var (
		only     *frontierSet
		distinct int
	)
	for _, e := range followable.from(n) {
		set := fr.byNode[e.Callee]
		if set == nil || set == fr.empty {
			continue
		}
		switch {
		case only == nil:
			only, distinct = set, 1
		case only != set:
			distinct = 2
		}
	}
	switch distinct {
	case 0:
		return fr.ci.decode(nil)
	case 1:
		return fr.ci.decode(only.bits)
	default:
		union := make([]uint64, fr.ci.words)
		for _, e := range followable.from(n) {
			if set := fr.byNode[e.Callee]; set != nil {
				for i := range set.bits {
					union[i] |= set.bits[i]
				}
			}
		}
		return fr.ci.decode(union)
	}
}
