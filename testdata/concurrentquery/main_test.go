package main

import (
	"io"
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
