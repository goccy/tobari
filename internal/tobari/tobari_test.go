package tobari

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
)

const (
	testFuncNum   = 300
	testDepFanout = 6
	testFileName  = "example.go"
)

var testCoverMetaOnce sync.Once

func testFuncName(i int) string {
	return fmt.Sprintf("example.com/pkg.Fn%d", i)
}

// setupTestCoverMeta registers coverage metadata for a synthetic package of
// testFuncNum functions where each function depends on the next
// testDepFanout functions, then returns a TraceEntry whose trace hit the
// first block of Fn0. Resolving candidate functions for that entry walks the
// whole dependency chain.
func setupTestCoverMeta(t *testing.T) *TraceEntry {
	t.Helper()
	testCoverMetaOnce.Do(func() {
		md := &Metadata{
			FileName:   testFileName,
			PkgPath:    "example.com/pkg",
			PkgName:    "pkg",
			ModulePath: "example.com",
		}
		suppDeps := make(map[string][]string)
		for i := 0; i < testFuncNum; i++ {
			name := testFuncName(i)
			md.Funcs = append(md.Funcs, &Function{
				Name: name,
				Blocks: []*Block{
					{
						Idx:      i,
						Start:    Pos{Line: i + 1, Col: 1},
						End:      Pos{Line: i + 1, Col: 10},
						NumStmts: 1,
					},
				},
			})
			for j := 1; j <= testDepFanout && i+j < testFuncNum; j++ {
				suppDeps[name] = append(suppDeps[name], testFuncName(i+j))
			}
		}
		suppDepsJSON, err := json.Marshal(suppDeps)
		if err != nil {
			panic(err)
		}
		AddCoverMeta(MarshalMetadata(md))
		AddSupplementaryDeps(string(suppDepsJSON))
		decodeRawMetas()
	})

	root := newTraceG(1)
	root.addCounter(blockID(testFileName, 0))
	return &TraceEntry{Name: "dep-resolution", Roots: []*TraceG{root}}
}

// depRefsLen returns the current DepRefs length of every registered function
// and fails the test if any DepRefs slice contains a nil element.
func depRefsLen(t *testing.T) map[string]int {
	t.Helper()
	funcMapMu.RLock()
	defer funcMapMu.RUnlock()
	ret := make(map[string]int, len(funcMap))
	for name, fn := range funcMap {
		for i, ref := range fn.DepRefs {
			if ref == nil {
				t.Fatalf("function %s has nil DepRefs element at index %d (len=%d)", name, i, len(fn.DepRefs))
			}
		}
		ret[name] = len(fn.DepRefs)
	}
	return ret
}

// Concurrent CoverprofileMap calls must not race on the shared Function
// dependency graph. Before the fix, resolveCandidateFuncMap appended to
// fn.DepRefs under a read lock, so concurrent calls corrupted the slice and
// crashed with a nil pointer dereference while recursing over DepRefs.
func TestCoverprofileMapConcurrentDepResolution(t *testing.T) {
	entry := setupTestCoverMeta(t)

	const (
		goroutineNum = 16
		iterations   = 300
	)
	var wg sync.WaitGroup
	for i := 0; i < goroutineNum; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				entry.CoverprofileMap()
			}
		}()
	}
	wg.Wait()

	depRefsLen(t)
}

// Repeated CoverprofileMap calls must not keep growing fn.DepRefs. Before the
// fix, every call re-appended the same resolved references because the
// visited set was per-call while DepRefs persisted on the shared Function.
func TestCoverprofileMapRepeatedCallsKeepDepRefsStable(t *testing.T) {
	entry := setupTestCoverMeta(t)

	entry.CoverprofileMap()
	before := depRefsLen(t)
	entry.CoverprofileMap()
	after := depRefsLen(t)

	for name, beforeLen := range before {
		if afterLen := after[name]; afterLen != beforeLen {
			t.Errorf("function %s: DepRefs grew from %d to %d across CoverprofileMap calls", name, beforeLen, afterLen)
		}
	}
}
