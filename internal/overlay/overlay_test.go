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
	if len(v.Replace) != 6 {
		t.Fatalf("unexpected replace contents: got: %v", len(v.Replace))
	}
}
