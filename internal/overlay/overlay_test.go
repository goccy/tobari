package overlay_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/goccy/tobari/internal/overlay"
)

func TestOverlay(t *testing.T) {
	o, err := overlay.Create(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.ReadFile(o)
	if err != nil {
		t.Fatal(err)
	}
	var v overlay.Overlay
	if err := json.Unmarshal(f, &v); err != nil {
		t.Fatal(err)
	}
	// 6 files: runtime/covercounter.go, runtime/tobari.go, testing/testing.go, testing/tobari.go,
	// testing/internal/testdeps/deps.go, testing/internal/testdeps/tobari.go
	if len(v.Replace) != 6 {
		t.Fatalf("unexpected replace contents: got: %v", len(v.Replace))
	}
}

func TestOverlayDeterministic(t *testing.T) {
	// With the new hash-based approach, each call should generate the same overlay
	// because the content is deterministic (based on template rendering results)
	o1, err := overlay.Create(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	f1, err := os.ReadFile(o1)
	if err != nil {
		t.Fatal(err)
	}

	o2, err := overlay.Create(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	f2, err := os.ReadFile(o2)
	if err != nil {
		t.Fatal(err)
	}

	if string(f1) != string(f2) {
		t.Fatal("expected same overlay content for deterministic hash-based approach, but got different")
	}
}
