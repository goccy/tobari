package tobari

import (
	"bytes"
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

// With passedBlocksOnly a scoped result holds exactly the blocks that were
// passed: no block of a dependent function, and no other block of the passed
// function, is added with a zero count. Without it the same trace expands
// through the dependency graph.
func TestCoverprofileMapPassedBlocksOnly(t *testing.T) {
	entry := setupTestCoverMeta(t)

	if got := len(entry.CoverprofileMap()); got <= 1 {
		t.Fatalf("default mode must add the blocks that could have been passed, got %d entries", got)
	}

	passedBlocksOnly = true
	t.Cleanup(func() { passedBlocksOnly = false })

	got := entry.CoverprofileMap()
	passed := blockID(testFileName, 0)
	if len(got) != 1 || got[passed] == nil {
		t.Fatalf("expected only the passed block %v, got %d entries", passed, len(got))
	}
	if got[passed].Count != 1 {
		t.Errorf("passed block count = %d, want 1", got[passed].Count)
	}
}

// Every count of the report must state passedBlocksOnly so that a consumer
// knows zero-count blocks are absent rather than out of scope, and no count
// may mention it otherwise, keeping default reports byte-identical.
func TestMarshalCoverPassedBlocksOnly(t *testing.T) {
	setupTestCoverMeta(t)
	root := newTraceG(2)
	root.addCounter(blockID(testFileName, 1))
	setEntry("marshal-passed-blocks-only", &TraceEntry{Name: "marshal-passed-blocks-only", Roots: []*TraceG{root}})

	type report struct {
		Metadata map[string]json.RawMessage   `json:"metadata"`
		Counts   []map[string]json.RawMessage `json:"counts"`
	}
	marshal := func(t *testing.T) report {
		t.Helper()
		b, err := MarshalCoverJSON()
		if err != nil {
			t.Fatal(err)
		}
		var r report
		if err := json.Unmarshal(b, &r); err != nil {
			t.Fatal(err)
		}
		if len(r.Counts) == 0 {
			t.Fatal("report has no counts")
		}
		return r
	}

	r := marshal(t)
	for _, c := range r.Counts {
		if v, exists := c["passedBlocksOnly"]; exists {
			t.Fatalf("default report must not carry passedBlocksOnly, got %s", v)
		}
	}
	if v, exists := r.Metadata["sources"]; exists {
		t.Fatalf("a runtime report has a single implicit source, got sources=%s", v)
	}

	passedBlocksOnly = true
	t.Cleanup(func() { passedBlocksOnly = false })

	for _, c := range marshal(t).Counts {
		if v := string(c["passedBlocksOnly"]); v != "true" {
			t.Fatalf("counts[].passedBlocksOnly = %q, want true", v)
		}
	}
	toon, err := MarshalCoverTOON()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(toon, []byte("passedBlocksOnly[")) || !bytes.Contains(toon, []byte("\n  marshal-passed-blocks-only\n")) {
		t.Errorf("TOON must list the passedBlocksOnly tests:\n%s", toon)
	}
}

// decodeRawMetas takes the mode from the instrumented packages' metadata.
func TestMetadataPassedBlocksOnlyRoundTrip(t *testing.T) {
	var md Metadata
	if err := json.Unmarshal([]byte(MarshalMetadata(&Metadata{FileName: "a.go", PassedBlocksOnly: true})), &md); err != nil {
		t.Fatal(err)
	}
	if !md.PassedBlocksOnly {
		t.Fatal("PassedBlocksOnly was lost in the metadata round trip")
	}
	if s := MarshalMetadata(&Metadata{FileName: "a.go"}); bytes.Contains([]byte(s), []byte("PassedBlocksOnly")) {
		t.Fatalf("default metadata must stay unchanged, got %s", s)
	}
}
