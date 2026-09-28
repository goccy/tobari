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
	fileID := RegisterFile("github.com/goccy/tobari/example/pkg/file.go", 64)
	trace := func() {
		Trace(fileID, 0, 1, 42)
	}
	trace()
	if allocs := testing.AllocsPerRun(1000, trace); allocs != 0 {
		t.Errorf("Trace allocated %v times per call on an already recorded block, want 0", allocs)
	}
}

// An instrumented package calls RegisterFile from its variable initializers
// and does not import this package, so the call can land before this package's
// own variables are initialized. RegisterFile has to work from the zero value.
func TestRegisterFileBeforePackageInit(t *testing.T) {
	fileMetasMu.Lock()
	savedMetas, savedIDs := fileMetas, fileIDs
	fileMetas, fileIDs = nil, nil
	fileMetasMu.Unlock()
	t.Cleanup(func() {
		fileMetasMu.Lock()
		fileMetas, fileIDs = savedMetas, savedIDs
		fileMetasMu.Unlock()
	})

	a := RegisterFile("a.go", 3)
	b := RegisterFile("b.go", 5)
	if a == b {
		t.Fatalf("distinct files got the same id %d", a)
	}
	if again := RegisterFile("a.go", 3); again != a {
		t.Fatalf("registering a.go again gave id %d, want %d", again, a)
	}
}

// BenchmarkTraceSameBlock measures the cost of hitting a single instrumented
// block repeatedly from one goroutine, which is what a hot loop in an
// instrumented program does.
func BenchmarkTraceSameBlock(b *testing.B) {
	ClearCounters()
	fileID := RegisterFile("github.com/goccy/tobari/example/pkg/file.go", 64)
	b.ReportAllocs()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Trace(fileID, 0, 1, 42)
	}
	b.StopTimer()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	b.ReportMetric(float64(after.NumGC-before.NumGC), "gc-cycles")
}
