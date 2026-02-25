package main

import (
	"sync/atomic"
	"testing"
)

func TestChannelExprSendEvaluatedOnce(t *testing.T) {
	resetSideEffects()
	done := make(chan struct{})
	go func() {
		<-sideSendCh
		close(done)
	}()
	SendWithSideEffect(1)
	<-done

	if got := atomic.LoadInt32(&sendCallCount); got != 1 {
		t.Fatalf("send channel expression evaluated %d times, want 1", got)
	}
}

func TestChannelExprRecvEvaluatedOnce(t *testing.T) {
	resetSideEffects()
	go func() {
		sideRecvCh <- 1
	}()
	_ = RecvWithSideEffect()

	if got := atomic.LoadInt32(&recvCallCount); got != 1 {
		t.Fatalf("recv channel expression evaluated %d times, want 1", got)
	}
}

func TestChannelExprSelectRecvEvaluatedOnce(t *testing.T) {
	resetSideEffects()
	go func() {
		sideSelectCh <- 1
	}()
	SelectRecvWithSideEffect()

	if got := atomic.LoadInt32(&selectCallCount); got != 1 {
		t.Fatalf("select recv channel expression evaluated %d times, want 1", got)
	}
}

func TestChannelExprSelectSendEvaluatedOnce(t *testing.T) {
	resetSideEffects()
	done := make(chan struct{})
	go func() {
		<-sideSelectSendCh
		close(done)
	}()
	SelectSendWithSideEffect()
	<-done

	if got := atomic.LoadInt32(&selectSendCallCount); got != 1 {
		t.Fatalf("select send channel expression evaluated %d times, want 1", got)
	}
}

func TestChannelExprRangeEvaluatedOnce(t *testing.T) {
	resetSideEffects()
	go func() {
		sideRangeCh <- 1
		sideRangeCh <- 2
		close(sideRangeCh)
	}()
	if got := RangeWithSideEffect(); got != 3 {
		t.Fatalf("RangeWithSideEffect() = %d, want 3", got)
	}
	if got := atomic.LoadInt32(&rangeCallCount); got != 1 {
		t.Fatalf("range channel expression evaluated %d times, want 1", got)
	}
}
