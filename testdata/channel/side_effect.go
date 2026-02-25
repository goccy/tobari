package main

import "sync/atomic"

var (
	sideSendCh       = make(chan int)
	sideRecvCh       = make(chan int)
	sideSelectCh     = make(chan int)
	sideRangeCh      = make(chan int)
	sideSelectSendCh = make(chan int)

	sendCallCount       int32
	recvCallCount       int32
	selectCallCount     int32
	rangeCallCount      int32
	selectSendCallCount int32
)

func resetSideEffects() {
	atomic.StoreInt32(&sendCallCount, 0)
	atomic.StoreInt32(&recvCallCount, 0)
	atomic.StoreInt32(&selectCallCount, 0)
	atomic.StoreInt32(&rangeCallCount, 0)
	atomic.StoreInt32(&selectSendCallCount, 0)

	sideSendCh = make(chan int)
	sideRecvCh = make(chan int)
	sideSelectCh = make(chan int)
	sideRangeCh = make(chan int)
	sideSelectSendCh = make(chan int)
}

func nextSendChan() chan int {
	atomic.AddInt32(&sendCallCount, 1)
	return sideSendCh
}

func nextRecvChan() chan int {
	atomic.AddInt32(&recvCallCount, 1)
	return sideRecvCh
}

func nextSelectChan() chan int {
	atomic.AddInt32(&selectCallCount, 1)
	return sideSelectCh
}

func nextSelectSendChan() chan int {
	atomic.AddInt32(&selectSendCallCount, 1)
	return sideSelectSendCh
}

func nextRangeChan() chan int {
	atomic.AddInt32(&rangeCallCount, 1)
	return sideRangeCh
}

func SendWithSideEffect(v int) {
	nextSendChan() <- v
}

func RecvWithSideEffect() int {
	return <-nextRecvChan()
}

func SelectRecvWithSideEffect() {
	select {
	case <-nextSelectChan():
	}
}

func SelectSendWithSideEffect() {
	select {
	case nextSelectSendChan() <- 1:
	}
}

func RangeWithSideEffect() int {
	sum := 0
	for v := range nextRangeChan() {
		sum += v
	}
	return sum
}
