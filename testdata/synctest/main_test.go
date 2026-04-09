package main

import (
	"testing"
	"testing/synctest"
)

func TestAddInBubble(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		if Add(2, 3) != 5 {
			t.Fatalf("Add(2,3) != 5")
		}
	})
}
