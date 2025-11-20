package tobari

import "github.com/goccy/tobari/internal/tobari"

type (
	// Mode corresponds to the mode in the coverprofile format.
	Mode     = tobari.Mode
	Metadata = tobari.Metadata
	Function = tobari.Function
	Block    = tobari.Block
	Pos      = tobari.Pos
)

const (
	// SetMode represents `set` mode.
	SetMode = tobari.SetMode
	// CountMode represents `count` mode.
	CountMode = tobari.CountMode
	// AtomicMode represents `atomic` mode.
	AtomicMode = tobari.AtomicMode
)

var (
	// Similar to ClearCounters in runtime/coverage, this resets the currently active counters.
	// It is intended to be called at the start of coverage measurement.
	ClearCounters = tobari.ClearCounters

	// Writes data in coverprofile format.
	// The resulting output can be directly used with `go tool cover`.
	WriteCoverProfile       = tobari.WriteCoverProfile
	WriteCoverProfileByName = tobari.WriteCoverProfileByName

	// When measuring coverage, wrap the function with Cover.
	Cover = tobari.Cover

	CoverWithName = tobari.CoverWithName
)
