package tobari

import (
	"bytes"
	"fmt"
	"maps"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
)

// recordTwoScopes records a scope "cleared" and a scope "kept" that share the
// worker goroutine 720, and returns the goroutine records only "cleared"
// reaches.
func recordTwoScopes(t *testing.T) []*TraceG {
	t.Helper()
	ClearCounters()

	coverAs("cleared", 700)
	hit(700, 0, 70)
	hit(700, 0, 71)
	hit(701, 700, 72) // a goroutine started by the cleared scope

	coverAs("kept", 710)
	hit(710, 0, 70)
	hit(710, 0, 73)

	// Both scopes send to the worker 720 over one channel.
	TraceChan(0xE, 720, false)
	TraceChan(0xE, 700, true)
	TraceChan(0xE, 710, true)
	hit(720, 0, 74)

	return []*TraceG{getG(700), getG(701)}
}

func coverprofileText(write func(w *bytes.Buffer)) string {
	var buf bytes.Buffer
	write(&buf)
	return buf.String()
}

// Clearing a name discards that name's result only: the results of other
// names and the process-wide counts stay exactly as they were.
func TestClearCountersByNameKeepsOtherResults(t *testing.T) {
	setupTestCoverMeta(t)
	recordTwoScopes(t)

	keptBefore := CoverEntriesByName("kept")
	processBefore := processCounts()
	allBefore := coverprofileText(func(w *bytes.Buffer) { WriteAllCoverprofile("count", w) })
	reportBefore := CollectCoverReportData().AllCounts

	ClearCountersByName("cleared")

	if got := CoverEntriesByName("cleared"); got != nil {
		t.Errorf("CoverEntriesByName returned %d entries for the cleared name", len(got))
	}
	if _, exists := CoverEntriesMap()["cleared"]; exists {
		t.Error("CoverEntriesMap still has the cleared name")
	}
	if got := coverprofileText(func(w *bytes.Buffer) { WriteCoverprofileByName("cleared", "count", w) }); got != "" {
		t.Errorf("WriteCoverprofileByName wrote a profile for the cleared name:\n%s", got)
	}

	if got := CoverEntriesByName("kept"); !reflect.DeepEqual(got, keptBefore) {
		t.Error("the result of the kept name changed")
	}
	if got, _ := countOf(CoverEntriesByName("kept"), 74); got != 1 {
		t.Errorf("the kept name lost the shared worker: count of block 74 = %d, want 1", got)
	}
	if got := processCounts(); !maps.Equal(got, processBefore) {
		t.Errorf("process-wide counts changed: got %v, want %v", got, processBefore)
	}
	if got := coverprofileText(func(w *bytes.Buffer) { WriteAllCoverprofile("count", w) }); got != allBefore {
		t.Error("WriteAllCoverprofile changed")
	}
	if got := CollectCoverReportData().AllCounts; !reflect.DeepEqual(got, reportBefore) {
		t.Error("CollectCoverReportData().AllCounts changed")
	}
}

// Clearing a name releases what its goroutine records held, except for the
// records that a scope of another name still reaches.
func TestClearCountersByNameReleasesRecords(t *testing.T) {
	setupTestCoverMeta(t)
	exclusive := recordTwoScopes(t)
	worker := getG(720)

	ClearCountersByName("cleared")

	for _, g := range exclusive {
		g.mu.RLock()
		counters, children := len(g.BlockCounterMap), len(g.Children)
		g.mu.RUnlock()
		if counters != 0 || children != 0 {
			t.Errorf("record of goroutine %d still holds %d counters and %d children", g.ID, counters, children)
		}
	}
	worker.mu.RLock()
	defer worker.mu.RUnlock()
	if got := worker.BlockCounterMap[blockID(testFileName, 74)]; got != 1 {
		t.Errorf("the shared worker lost its counters: count of block 74 = %d, want 1", got)
	}
}

// A goroutine of a cleared scope that is still running keeps contributing to
// the process-wide counts, but to no scope; a new scope of the cleared name
// starts from nothing.
func TestClearCountersByNameThenRecordAgain(t *testing.T) {
	setupTestCoverMeta(t)
	recordTwoScopes(t)

	ClearCountersByName("cleared")

	hit(701, 700, 75)
	if got := processCounts()[blockID(testFileName, 75)]; got != 1 {
		t.Errorf("process-wide count of block 75 = %d, want 1", got)
	}
	if got := CoverEntriesByName("cleared"); got != nil {
		t.Errorf("a goroutine of the cleared scope recorded into the cleared name: %d entries", len(got))
	}

	coverAs("cleared", 730)
	hit(730, 0, 76)
	entries := CoverEntriesByName("cleared")
	if got, _ := countOf(entries, 76); got != 1 {
		t.Errorf("count of block 76 = %d, want 1", got)
	}
	for _, idx := range []int{72, 75} {
		if got, _ := countOf(entries, idx); got != 0 {
			t.Errorf("the new scope reports block %d of the cleared scope with count %d", idx, got)
		}
	}
}

// Every scope of a name is cleared, whichever call site started it.
func TestClearCountersByNameClearsEveryCallSite(t *testing.T) {
	setupTestCoverMeta(t)
	ClearCounters()

	coverAtSiteAs("multi", "multi:site1", 740)
	hit(740, 0, 77)
	coverAtSiteAs("multi", "multi:site2", 741)
	hit(741, 0, 78)

	ClearCountersByName("multi")

	if got := CoverEntriesByName("multi"); got != nil {
		t.Errorf("CoverEntriesByName returned %d entries after clearing every call site", len(got))
	}
}

// ClearCounters resets the counts that ClearCountersByName kept for the whole
// process as well.
func TestClearCountersResetsClearedCounts(t *testing.T) {
	setupTestCoverMeta(t)
	recordTwoScopes(t)
	ClearCountersByName("cleared")

	ClearCounters()

	if got := processCounts(); len(got) != 0 {
		t.Errorf("process-wide counts after ClearCounters = %v, want none", got)
	}
}

// Clearing names while other goroutines record, link over channels, start
// scopes and query must neither lose nor double any count of the whole
// process.
func TestClearCountersByNameConcurrently(t *testing.T) {
	setupTestCoverMeta(t)
	ClearCounters()

	const (
		recorders = 8
		rounds    = 200
		hitsPer   = 5
	)
	// Cover takes the goroutine ID from testGID, so scopes start one at a time.
	var coverMu sync.Mutex
	var nextGID atomic.Uint64
	nextGID.Store(10000)

	var wg sync.WaitGroup
	for i := range recorders {
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := fmt.Sprintf("concurrent-%d", i%3)
			for range rounds {
				root := nextGID.Add(1)
				coverMu.Lock()
				coverAs(name, root)
				coverMu.Unlock()
				child := nextGID.Add(1)
				for range hitsPer {
					hit(root, 0, 80)
					hit(child, root, 80)
				}
				// The child sends to the worker that every recorder shares.
				TraceChan(0xF, 1, false)
				TraceChan(0xF, child, true)
				ClearCountersByName(name)
			}
		}()
	}
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range rounds {
				CoverEntriesMap()
				processCounts()
				hit(1, 0, 81)
			}
		}()
	}
	wg.Wait()

	counts := processCounts()
	if got, want := counts[blockID(testFileName, 80)], recorders*rounds*hitsPer*2; got != want {
		t.Errorf("process-wide count of block 80 = %d, want %d", got, want)
	}
	if got, want := counts[blockID(testFileName, 81)], 4*rounds; got != want {
		t.Errorf("process-wide count of block 81 = %d, want %d", got, want)
	}
}
