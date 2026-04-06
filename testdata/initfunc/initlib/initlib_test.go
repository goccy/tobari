package initlib_test

import (
	"testing"

	"example.com/initfunc/initlib"
)

func TestTransform(t *testing.T) {
	if got := initlib.Transform("x"); got != "[x]" {
		t.Errorf("Transform(x) = %q, want [x]", got)
	}
}
