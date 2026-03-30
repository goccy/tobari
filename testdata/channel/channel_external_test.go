package main

import "testing"

func TestSendExternalRange(t *testing.T) {
	if got := SendExternalRange([]int{10, 20, 30}); got != 60 {
		t.Fatalf("SendExternalRange() = %d, want 60", got)
	}
}
