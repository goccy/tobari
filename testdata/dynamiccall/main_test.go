package main

import (
	"testing"

	"example.com/dynamiccall/work"
)

// TestLazyInit exercises only work.Do. Its scoped coverage must include
// initConfig (it flows into the sync.Once that Do fires) but must not
// include Extra or RunHandlers, which are unreachable from Do even though
// Extra is an address-taken func() like the one the Once invokes.
func TestLazyInit(t *testing.T) {
	if got := work.Do(); got != "initialized" {
		t.Fatalf("unexpected config: %q", got)
	}
}
