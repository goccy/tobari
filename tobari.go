package tobari

import (
	"fmt"
	"io"
	"runtime"

	"github.com/goccy/tobari/internal/tobari"
)

// Mode corresponds to the mode in the coverprofile format.
type Mode string

const (
	// SetMode represents `set` mode.
	SetMode Mode = "set"
	// CountMode represents `count` mode.
	CountMode Mode = "count"
	// AtomicMode represents `atomic` mode.
	AtomicMode Mode = "atomic"
)

// EnableCoverageCounting enables the counting functionality necessary for calculating coverage.
// Since it is enabled by default, it is assumed that this function is called after DisableCoverageCounting has been invoked.
func EnableCoverageCounting() {
	tobari.EnableCoverageCounting()
}

// DisableCoverageCounting disables the counting functionality required for calculating coverage.
// Since it is enabled by default, this is used in scenarios such as production environments where you want to disable the counting logic to avoid performance impact.
func DisableCoverageCounting() {
	tobari.DisableCoverageCounting()
}

// ClearCounters similar to ClearCounters in runtime/coverage, this resets the currently active counters.
// It is intended to be called at the start of coverage measurement.
func ClearCounters() {
	tobari.ClearCounters()
}

// Cover this feature is used to scoped coverage measurement.
// While `runtime/coverage` measures coverage across entire specified packages,
// using this feature allows coverage to be measured for specific functions.
// For example, by implementing this feature in an HTTP server middleware or a gRPC server interceptor,
// you can measure coverage on a per-method basis for each request.
func Cover(fn func()) {
	cover("", fn)
}

// CoverWithName when measuring coverage, you can assign a name to the measurement scope.
// By using the WriteCoverProfileByName method when writing out the results,
// you can filter and output only the coverage data associated with the named measurement target.
func CoverWithName(name string, fn func()) {
	cover(name, fn)
}

func cover(name string, fn func()) {
	// Identifies the source code positions where Cover or CoverWithName is being used.
	_, file, line, _ := runtime.Caller(2)
	scopeID := fmt.Sprintf("%s:%s:%d", name, file, line)
	waitCh := make(chan error)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				waitCh <- fmt.Errorf("tobari: recovered: %v", r)
			}
		}()

		tobari.Cover(name, scopeID)
		fn()
		waitCh <- nil
	}()
	if err := <-waitCh; err != nil {
		// Re-throws the panic that occurred inside fn().
		panic(err)
	}
}

// WriteAllCoverProfile uses the coverage counts at the time this method is called to write coverage data for all locations subject to coverage measurement to the provided io.Writer value.
// Since the data is written in the coverprofile format, you can directly use with `go tool cover`.
// The resulting data is the same as the data obtained by decoding the output from executing WriteMeta and WriteCounters using runtime/coverage.
func WriteAllCoverProfile(mode Mode, w io.Writer) {
	tobari.WriteAllCoverProfile(string(mode), w)
}

// WriteCoverProfile writes coverprofile data based on the coverage range measured by Cover or CoverWithName.
// Parts that are not invoked via the Cover or CoverWithName methods are not counted.
// Also, unreachable ranges are calculated based on the paths that were actually called.
func WriteCoverProfile(mode Mode, w io.Writer) {
	tobari.WriteCoverProfile(string(mode), w)
}

// WriteCoverProfileByName it basically works the same as WriteCoverProfile,
// but additionally allows you to target only the ranges with the specified name.
func WriteCoverProfileByName(name string, mode Mode, w io.Writer) {
	tobari.WriteCoverProfileByName(name, string(mode), w)
}

// CoverProfileMap if coverage was measured using CoverWithName,
// it outputs the correspondence between the names and the coverprofile data for each name.
func CoverProfileMap(mode Mode) map[string]string {
	return tobari.CoverProfileMap(string(mode))
}
