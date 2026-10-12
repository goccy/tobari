package tobari

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// testGID is the goroutine ID that Cover sees in this test binary. Trace and
// TraceChan take goroutine IDs as arguments, so tests address goroutine records
// directly; Cover reads the current one through the GID function instead.
var testGID atomic.Uint64

func init() {
	SetGIDFunc(testGID.Load)
}

// coverAs starts a scope named name whose root is the goroutine record gid,
// the way the public CoverWithName does on the goroutine it spawns.
func coverAs(name string, gid uint64) {
	coverAtSiteAs(name, name, gid)
}

// coverAtSiteAs is coverAs for a scope identified by entryID, which the public
// API derives from the name and the call site.
func coverAtSiteAs(name, entryID string, gid uint64) {
	testGID.Store(gid)
	Cover(name, entryID)
}

// hit records one pass through block idx of the synthetic test package on the
// goroutine gid, whose parent goroutine is pgid.
func hit(gid, pgid uint64, idx int) {
	Trace(testFileName, pgid, gid, idx, idx+1, idx+1, 1, 10, 1)
}

// countOf returns the count of block idx of the synthetic test package in
// entries, and whether the block is present at all.
func countOf(entries []*CoverEntry, idx int) (int, bool) {
	for _, e := range entries {
		if e.FileName == testFileName && e.StartLine == idx+1 {
			return e.Count, true
		}
	}
	return 0, false
}

// profileLine is the coverprofile line of block idx of the synthetic test
// package with the given count.
func profileLine(idx, count int) string {
	return fmt.Sprintf("%s:%d.1,%d.10 1 %d\n", testFileName, idx+1, idx+1, count)
}

// Every goroutine record is also registered on its own, so the process-wide
// counts must add each goroutine's counters exactly once. Walking each
// registered goroutine together with its descendants counted a goroutine once
// for itself and once more for every ancestor.
func TestProcessCountsCountEachGoroutineOnce(t *testing.T) {
	setupTestCoverMeta(t)
	ClearCounters()

	hit(100, 0, 5)
	hit(101, 100, 5) // child of 100
	hit(102, 101, 5) // grandchild of 100

	if got := CollectCoverReportData().AllCounts[5]; got != 3 {
		t.Errorf("CollectCoverReportData().AllCounts[5] = %d, want 3", got)
	}
	var buf bytes.Buffer
	WriteAllCoverprofile("count", &buf)
	if want := profileLine(5, 3); !strings.Contains(buf.String(), want) {
		t.Errorf("WriteAllCoverprofile does not contain %q:\n%s", want, buf.String())
	}
}

// WriteCoverprofile reports the union of every scope. A block that one scope
// passed must keep its count even when another scope only lists it as a block
// it could have passed (count 0).
func TestWriteCoverprofileKeepsCountsAcrossScopes(t *testing.T) {
	setupTestCoverMeta(t)
	ClearCounters()

	// Fn0 depends on Fn1, so scope a lists block 1 as a candidate with count 0
	// while scope b passed it.
	coverAs("a", 200)
	hit(200, 0, 0)
	coverAs("b", 201)
	hit(201, 0, 1)

	// The scopes are merged in map order, so repeat to cover both orders.
	for range 20 {
		var buf bytes.Buffer
		WriteCoverprofile("count", &buf)
		for idx, want := range map[int]int{0: 1, 1: 1} {
			if line := profileLine(idx, want); !strings.Contains(buf.String(), line) {
				t.Fatalf("WriteCoverprofile does not contain %q:\n%s", line, buf.String())
			}
		}
	}
}

// Scopes started under one name from different call sites belong to that name
// together, so every by-name query must report all of them.
func TestScopesWithSameNameAreMerged(t *testing.T) {
	setupTestCoverMeta(t)
	ClearCounters()

	coverAtSiteAs("dup", "dup:site1", 300)
	hit(300, 0, 10)
	coverAtSiteAs("dup", "dup:site2", 301)
	hit(301, 0, 20)

	check := func(what string, entries []*CoverEntry) {
		t.Helper()
		for _, idx := range []int{10, 20} {
			if got, _ := countOf(entries, idx); got != 1 {
				t.Fatalf("%s: count of block %d = %d, want 1", what, idx, got)
			}
		}
	}
	// The scopes live in a map, so repeat to cover every iteration order.
	for range 20 {
		check("CoverEntriesByName", CoverEntriesByName("dup"))
		check("CoverEntriesMap", CoverEntriesMap()["dup"])
		var buf bytes.Buffer
		WriteCoverprofileByName("dup", "count", &buf)
		for _, idx := range []int{10, 20} {
			if line := profileLine(idx, 1); !strings.Contains(buf.String(), line) {
				t.Fatalf("WriteCoverprofileByName does not contain %q:\n%s", line, buf.String())
			}
		}
	}
}

// A goroutine linked to several scopes of one name (a worker serving several
// requests) holds its counters once, so the name's result must count them
// once rather than once per scope.
func TestGoroutineSharedByScopesIsCountedOnce(t *testing.T) {
	setupTestCoverMeta(t)
	ClearCounters()

	coverAs("shared", 400)
	coverAs("shared", 401)
	// Both scopes send to the worker 402 over one channel.
	TraceChan(0xA, 402, false)
	TraceChan(0xA, 400, true)
	TraceChan(0xA, 401, true)
	hit(402, 0, 30)
	hit(402, 0, 30)

	if got, _ := countOf(CoverEntriesByName("shared"), 30); got != 2 {
		t.Errorf("count of block 30 = %d, want 2", got)
	}
}

// blockingWriter blocks the first Write until release is closed, standing in
// for a slow destination such as a network connection.
type blockingWriter struct {
	once    sync.Once
	entered chan struct{}
	release chan struct{}
}

func newBlockingWriter() *blockingWriter {
	return &blockingWriter{entered: make(chan struct{}), release: make(chan struct{})}
}

func (w *blockingWriter) Write(p []byte) (int, error) {
	w.once.Do(func() {
		close(w.entered)
		<-w.release
	})
	return len(p), nil
}

// Writing a coverprofile to a slow destination must not stop the program from
// recording: starting scopes, recording new goroutines and linking goroutines
// over channels all have to proceed while the profile is being written.
func TestWritingCoverprofileDoesNotBlockRecording(t *testing.T) {
	setupTestCoverMeta(t)

	for _, tc := range []struct {
		name  string
		write func(w *blockingWriter)
	}{
		{"WriteCoverprofile", func(w *blockingWriter) { WriteCoverprofile("set", w) }},
		{"WriteCoverprofileByName", func(w *blockingWriter) { WriteCoverprofileByName("slow", "set", w) }},
		{"WriteAllCoverprofile", func(w *blockingWriter) { WriteAllCoverprofile("set", w) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ClearCounters()
			coverAs("slow", 600)
			hit(600, 0, 50)

			w := newBlockingWriter()
			written := make(chan struct{})
			go func() {
				tc.write(w)
				close(written)
			}()
			<-w.entered

			recorded := make(chan struct{})
			go func() {
				coverAs(fmt.Sprintf("during-%s", tc.name), 601)
				hit(601, 0, 51)
				hit(602, 601, 52) // a goroutine recorded for the first time
				TraceChan(0xD, 603, false)
				TraceChan(0xD, 601, true)
				close(recorded)
			}()
			select {
			case <-recorded:
			case <-time.After(5 * time.Second):
				t.Error("recording was blocked while the coverprofile was being written")
			}
			close(w.release)
			<-written
			<-recorded
		})
	}
}

// Channel links can make goroutine records cyclic: two goroutines that send to
// each other each become the other's child. Queries walking such a cycle from
// different goroutines at once, while those goroutines keep recording, must
// not deadlock.
func TestConcurrentQueriesOverCyclicLinksDoNotDeadlock(t *testing.T) {
	setupTestCoverMeta(t)
	ClearCounters()

	coverAs("cycle-a", 500)
	coverAs("cycle-b", 501)
	TraceChan(0xB, 501, false)
	TraceChan(0xB, 500, true) // 500 -> 501
	TraceChan(0xC, 500, false)
	TraceChan(0xC, 501, true) // 501 -> 500

	stop := make(chan struct{})
	var wg sync.WaitGroup
	for _, gid := range []uint64{500, 501} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					hit(gid, 0, 60)
				}
			}
		}()
	}
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					CoverEntriesMap()
				}
			}
		}()
	}
	time.Sleep(2 * time.Second)
	close(stop)

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("queries over cyclic goroutine links deadlocked")
	}
}
