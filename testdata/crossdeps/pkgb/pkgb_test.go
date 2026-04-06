package pkgb_test

import (
	"testing"

	"example.com/crossdeps/pkgb"
)

func TestDouble(t *testing.T) {
	if got := pkgb.Double(3); got != 6 {
		t.Errorf("Double(3) = %d, want 6", got)
	}
}
