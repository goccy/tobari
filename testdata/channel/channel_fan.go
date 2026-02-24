package main

// Fan-out: init goroutine spawns 3 workers that read from one channel.
var (
	fanoutIn  = make(chan int)
	fanoutOut = make(chan int)
)

// Range over channel: init goroutine sums values until channel is closed.
var (
	rangeCh     = make(chan int)
	rangeResult = make(chan int)
)

func init() {
	for range 3 {
		go func() {
			v := <-fanoutIn
			fanoutOut <- v * v
		}()
	}
	go func() {
		sum := 0
		for v := range rangeCh {
			sum += v
		}
		rangeResult <- sum
	}()
}

// Request-Response: init goroutine receives a request and sends response.
type request struct {
	value  int
	respCh chan int
}

var requestCh = make(chan request)

func init() {
	go func() {
		req := <-requestCh
		req.respCh <- req.value + 100
	}()
}

func SendFanout(v int) int {
	fanoutIn <- v
	return <-fanoutOut
}

func SendRange(values []int) int {
	for _, v := range values {
		rangeCh <- v
	}
	close(rangeCh)
	return <-rangeResult
}

func SendRequest(v int) int {
	respCh := make(chan int)
	requestCh <- request{value: v, respCh: respCh}
	return <-respCh
}
