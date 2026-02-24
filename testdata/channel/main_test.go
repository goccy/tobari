package main

import "testing"

func TestSendUnbuffered(t *testing.T) {
	if got := SendUnbuffered(5); got != 10 {
		t.Errorf("SendUnbuffered(5) = %d, want 10", got)
	}
}

func TestSendBuffered(t *testing.T) {
	if got := SendBuffered(1, 2, 3); got != 6 {
		t.Errorf("SendBuffered(1, 2, 3) = %d, want 6", got)
	}
}

func TestSendSelectInt(t *testing.T) {
	if got := SendSelectInt(7); got != 21 {
		t.Errorf("SendSelectInt(7) = %d, want 21", got)
	}
}

func TestSignalDone(t *testing.T) {
	SignalDone()
}

func TestSendPipeline(t *testing.T) {
	// (v + 1) * 2 = (4 + 1) * 2 = 10
	if got := SendPipeline(4); got != 10 {
		t.Errorf("SendPipeline(4) = %d, want 10", got)
	}
}

func TestSendFanout(t *testing.T) {
	if got := SendFanout(3); got != 9 {
		t.Errorf("SendFanout(3) = %d, want 9", got)
	}
}

func TestSendRange(t *testing.T) {
	if got := SendRange([]int{1, 2, 3, 4}); got != 10 {
		t.Errorf("SendRange([1,2,3,4]) = %d, want 10", got)
	}
}

func TestSendRequest(t *testing.T) {
	if got := SendRequest(42); got != 142 {
		t.Errorf("SendRequest(42) = %d, want 142", got)
	}
}
