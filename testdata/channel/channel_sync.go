package main

// Done/signal pattern: init goroutine waits for done, then sends ack.
var (
	doneCh = make(chan struct{})
	ackCh  = make(chan struct{})
)

// Pipeline: init goroutine receives, adds 1, then multiplies by 2.
var (
	pipelineIn  = make(chan int)
	pipelineMid = make(chan int)
	pipelineOut = make(chan int)
)

func init() {
	go func() {
		<-doneCh
		ackCh <- struct{}{}
	}()
	go func() {
		v := <-pipelineIn
		pipelineMid <- v + 1
	}()
	go func() {
		v := <-pipelineMid
		pipelineOut <- v * 2
	}()
}

func SignalDone() {
	doneCh <- struct{}{}
	<-ackCh
}

func SendPipeline(v int) int {
	pipelineIn <- v
	return <-pipelineOut
}
