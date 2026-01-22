package overlay_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/goccy/tobari/internal/overlay"
)

func TestOverlay(t *testing.T) {
	o, err := overlay.Create(t.Context(), false)
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
	if len(v.Replace) != 6 {
		t.Fatalf("unexpected replace contents: got: %v", len(v.Replace))
	}
}

func TestOverlayWithoutFix(t *testing.T) {
	// When fix=false, each call should generate a different overlay
	o1, err := overlay.Create(t.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	f1, err := os.ReadFile(o1)
	if err != nil {
		t.Fatal(err)
	}

	o2, err := overlay.Create(t.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	f2, err := os.ReadFile(o2)
	if err != nil {
		t.Fatal(err)
	}

	if string(f1) == string(f2) {
		t.Fatal("expected different overlay content for fix=false, but got the same")
	}
}

func TestOverlayWithFix(t *testing.T) {
	// When fix=true, each call should generate the same overlay
	o1, err := overlay.Create(t.Context(), true)
	if err != nil {
		t.Fatal(err)
	}
	f1, err := os.ReadFile(o1)
	if err != nil {
		t.Fatal(err)
	}

	o2, err := overlay.Create(t.Context(), true)
	if err != nil {
		t.Fatal(err)
	}
	f2, err := os.ReadFile(o2)
	if err != nil {
		t.Fatal(err)
	}

	if string(f1) != string(f2) {
		t.Fatal("expected same overlay content for fix=true, but got different")
	}
}
