package main

import (
	"fmt"

	"example.com/concurrentquery/worker"
)

func main() {
	fmt.Println(worker.Run(8))
}
