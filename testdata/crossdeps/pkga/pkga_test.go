package pkga_test

import (
	"testing"

	"example.com/crossdeps/pkga"
)

func TestQuadruple(t *testing.T) {
	if got := pkga.Quadruple(3); got != 12 {
		t.Errorf("Quadruple(3) = %d, want 12", got)
	}
}
