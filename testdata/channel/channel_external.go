package main

import "example.com/channel/chprovider"

// externalRangeCh is a channel created by an external package.
// A goroutine ranges over it in init(), and the test only sends to it.
// Without _afterRecvChan wrapping on the range expression, the cross-goroutine
// coverage link is broken and the receiver's coverage won't be traced.
var (
	externalRangeCh     = chprovider.NewChan()
	externalRangeResult = make(chan int)
)

func init() {
	go func() {
		sum := 0
		for v := range externalRangeCh {
			sum += v
		}
		externalRangeResult <- sum
	}()
}

// SendExternalRange sends values through the external channel and returns the sum
// computed by the receiver goroutine. The test calls only this function.
func SendExternalRange(values []int) int {
	for _, v := range values {
		externalRangeCh <- v
	}
	close(externalRangeCh)
	return <-externalRangeResult
}
