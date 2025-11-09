package tobari

import "github.com/goccy/tobari/internal/tobari"

type (
	// Mode corresponds to the mode in the coverprofile format.
	Mode = tobari.Mode
)

const (
	// SetMode represents `set` mode.
	SetMode = tobari.SetMode
	// CountMode represents `count` mode.
	CountModoe = tobari.CountMode
	// AtomicMode represents `atomic` mode.
	AtomicMode = tobari.AtomicMode
)

var (
	// Similar to ClearCounters in runtime/coverage, this resets the currently active counters.
	// It is intended to be called at the start of coverage measurement.
	ClearCounters = tobari.ClearCounters

	// Writes data in coverprofile format.
	// The resulting output can be directly used with `go tool cover`.
	WriteCoverProfile = tobari.WriteCoverProfile

	// When measuring coverage, wrap the function with Cover.
	Cover = tobari.Cover
)

var (
	// This function is an API used at measurement points.
	// It is used to register a function that retrieves the goroutine ID.
	SetGIDFunc = tobari.SetGIDFunc

	// This function is an API used at measurement points.
	// It is used at coverage measurement points.
	Trace = tobari.Trace
)
