package main

import (
	"testing"

	"example.com/thirdparty/handler"
)

// TestDirect calls handler.Direct which calls store.Save directly (no extlib).
// The dependency handler.Direct → store.Save is always detected because both
// packages have SSA bodies built.
func TestDirect(t *testing.T) {
	result := handler.Direct("hello")
	if result != "saved:hello" {
		t.Fatal("unexpected:", result)
	}
}

// TestCallback calls handler.Handle with empty data so extlib.Process does NOT
// invoke the callback (store.Save). The dependency handler.Handle → store.Save
// goes through a non-target package (extlib) via function-value callback.
// Without extlib's SSA body, RTA cannot see the indirect call site fn(data)
// inside extlib.Process, so the dependency is lost.
func TestCallback(t *testing.T) {
	result := handler.Handle("")
	if result != "" {
		t.Fatal("unexpected:", result)
	}
}

// TestInterface calls handler.HandleTransform with empty data so
// extlib.RunTransform does NOT invoke the Transformer interface method.
// The dependency handler.HandleTransform → (*store.Formatter).Transform goes
// through a non-target package (extlib) via interface dispatch (Invoke).
// Without extlib's SSA body, RTA cannot see the Invoke instruction
// t.Transform(data) inside extlib.RunTransform, so the dependency is lost.
func TestInterface(t *testing.T) {
	result := handler.HandleTransform("")
	if result != "" {
		t.Fatal("unexpected:", result)
	}
}
