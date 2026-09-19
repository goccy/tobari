package tobari

import (
	"runtime"
	"testing"
)

// Trace runs once per executed block of an instrumented program, so a hot loop
// calls it for the same block over and over. Any allocation there turns into
// GC pressure proportional to the loop count, so hitting a block that the
// goroutine has already recorded must not allocate.
func TestTraceSameBlockDoesNotAllocate(t *testing.T) {
	ClearCounters()
	trace := func() {
		Trace("github.com/goccy/tobari/example/pkg/file.go", 0, 1, 42, 10, 12, 2, 3, 1)
	}
	trace()
	if allocs := testing.AllocsPerRun(1000, trace); allocs != 0 {
		t.Errorf("Trace allocated %v times per call on an already recorded block, want 0", allocs)
	}
}

// BenchmarkTraceSameBlock measures the cost of hitting a single instrumented
// block repeatedly from one goroutine, which is what a hot loop in an
// instrumented program does.
func BenchmarkTraceSameBlock(b *testing.B) {
	ClearCounters()
	b.ReportAllocs()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Trace("github.com/goccy/tobari/example/pkg/file.go", 0, 1, 42, 10, 12, 2, 3, 1)
	}
	b.StopTimer()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	b.ReportMetric(float64(after.NumGC-before.NumGC), "gc-cycles")
}
