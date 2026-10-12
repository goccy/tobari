package main

import (
	"fmt"
	"io"
	"reflect"
	"sync"
	"testing"

	"github.com/goccy/tobari"

	"example.com/concurrentquery/worker"
)

// TestConcurrentCoverageQueries mirrors a long-running server that records
// traces and then exports coverage from many goroutines at once: traces are
// recorded first, then concurrent goroutines repeatedly render the
// coverprofile. Each render resolves the supplementary dependency graph, so
// any unsynchronized mutation of shared coverage-runtime state surfaces here.
// The e2e suite runs this test under the race detector, which the outer
// `go test -race` cannot do for the runtime injected into child binaries.
func TestConcurrentCoverageQueries(t *testing.T) {
	for range 4 {
		tobari.Cover(func() {
			if got := worker.Run(8); got == 0 {
				t.Fatal("unexpected zero result from worker.Run")
			}
		})
	}

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				tobari.WriteCoverprofile(tobari.SetMode, io.Discard)
			}
		}()
	}
	wg.Wait()
}

// TestConcurrentScopesQueriesAndClears mirrors a server that measures every
// request under a name and exports and discards that name's result right after
// the request, while requests of the same and of other names are still being
// measured and other goroutines export coverage. Scopes, queries and clears
// all run at once under the race detector.
func TestConcurrentScopesQueriesAndClears(t *testing.T) {
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := fmt.Sprintf("request-%d", i%4)
			for range 50 {
				tobari.CoverWithName(name, func() {
					if got := worker.Run(8); got == 0 {
						t.Error("unexpected zero result from worker.Run")
					}
				})
				_ = tobari.CoverprofileByName(name, tobari.SetMode)
				tobari.ClearCountersByName(name)
			}
		}()
	}
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				tobari.WriteCoverprofile(tobari.SetMode, io.Discard)
				_ = tobari.CoverprofileMap(tobari.SetMode)
				tobari.WriteAllCoverprofile(tobari.SetMode, io.Discard)
				_ = tobari.CollectCoverReport()
			}
		}()
	}
	wg.Wait()
}

// TestClearCountersByName checks that clearing one name discards its result
// and leaves the result of another name unchanged.
func TestClearCountersByName(t *testing.T) {
	tobari.ClearCounters()
	tobari.CoverWithName("kept", func() { worker.Run(8) })
	tobari.CoverWithName("cleared", func() { worker.Run(8) })
	if len(tobari.CoverprofileByName("cleared", tobari.CountMode).Entries) == 0 {
		t.Fatal("the scope to clear recorded nothing")
	}
	kept := tobari.CoverprofileByName("kept", tobari.CountMode)

	tobari.ClearCountersByName("cleared")

	if got := tobari.CoverprofileByName("cleared", tobari.CountMode).Entries; len(got) != 0 {
		t.Errorf("the cleared name still has %d entries", len(got))
	}
	if _, exists := tobari.CoverprofileMap(tobari.CountMode)["cleared"]; exists {
		t.Error("CoverprofileMap still has the cleared name")
	}
	if got := tobari.CoverprofileByName("kept", tobari.CountMode); !reflect.DeepEqual(got, kept) {
		t.Error("the result of the kept name changed")
	}
}
