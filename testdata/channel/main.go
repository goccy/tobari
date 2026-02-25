package main

import "fmt"

// Unbuffered channel: init goroutine receives, doubles, and sends back.
var (
	unbufferedIn  = make(chan int)
	unbufferedOut = make(chan int)
)

// Buffered channel: init goroutine waits for signal, reads buffered values, and sums them.
var (
	bufferedCh     = make(chan int, 3)
	bufferedSignal = make(chan struct{})
	bufferedResult = make(chan int)
)

func init() {
	go func() {
		v := <-unbufferedIn
		unbufferedOut <- v * 2
	}()
	go func() {
		<-bufferedSignal
		sum := 0
		for i := 0; i < 3; i++ {
			sum += <-bufferedCh
		}
		bufferedResult <- sum
	}()
}

// Select: init goroutine receives from one of two channels.
var (
	selectIntCh  = make(chan int)
	selectStrCh  = make(chan string)
	selectResult = make(chan int)
)

func init() {
	go func() {
		select {
		case v := <-selectIntCh:
			selectResult <- v * 3
		case s := <-selectStrCh:
			selectResult <- len(s)
		}
	}()
}

func SendUnbuffered(v int) int {
	unbufferedIn <- v
	return <-unbufferedOut
}

func SendBuffered(a, b, c int) int {
	bufferedCh <- a
	bufferedCh <- b
	bufferedCh <- c
	bufferedSignal <- struct{}{}
	return <-bufferedResult
}

func SendSelectInt(v int) int {
	selectIntCh <- v
	return <-selectResult
}

func main() {
	fmt.Println(SendUnbuffered(5))
	fmt.Println(SendBuffered(1, 2, 3))
	fmt.Println(SendSelectInt(7))
}
