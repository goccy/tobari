package lib

import "testing"

// TestDirect calls Direct which calls store.Save directly (no extlib).
func TestDirect(t *testing.T) {
	result := Direct("hello")
	if result != "saved:hello" {
		t.Fatal("unexpected:", result)
	}
}

// TestCallback calls Handle with empty data so extlib.Process does NOT
// invoke the callback. With correct suppDeps, store.Save should appear
// as a dependency (count=0).
func TestCallback(t *testing.T) {
	result := Handle("")
	if result != "" {
		t.Fatal("unexpected:", result)
	}
}

// TestInterface calls HandleTransform with empty data so extlib.RunTransform
// does NOT invoke the interface method. With correct suppDeps,
// (*store.Formatter).Transform should appear as a dependency (count=0).
func TestInterface(t *testing.T) {
	result := HandleTransform("")
	if result != "" {
		t.Fatal("unexpected:", result)
	}
}
