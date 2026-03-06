package main

import (
	"fmt"
	"strconv"

	"example.com/crosspkg/container"
	"example.com/crosspkg/pipeline"
	"example.com/crosspkg/service"
	"example.com/crosspkg/transform"
)

// UpperProcessor implements service.Processor.
type UpperProcessor struct{}

func (u UpperProcessor) Process(input string) string {
	return "UPPER:" + input
}

func main() {
	// Generic container
	pair := container.NewPair(42, "hello")
	swapped := pair.Swap()
	fmt.Println(swapped.First, swapped.Second)

	// Nested generic
	stack := &container.Stack[container.Pair[int, string]]{}
	stack.Push(pair)
	if item, ok := stack.Pop(); ok {
		fmt.Println(item.First)
	}

	// Higher-order generic functions
	nums := []int{1, 2, 3, 4, 5}
	strs := transform.Map(nums, strconv.Itoa)
	evens := transform.Filter(nums, func(n int) bool { return n%2 == 0 })
	sum := transform.Reduce(nums, 0, func(acc, n int) int { return acc + n })
	fmt.Println(strs, evens, sum)

	// Function composition
	doubleStr := transform.Compose(
		func(n int) int { return n * 2 },
		strconv.Itoa,
	)
	fmt.Println(doubleStr(21))

	// Interface + Generic
	reg := service.NewRegistry[UpperProcessor]()
	reg.Register("upper", UpperProcessor{})
	fmt.Println(reg.Execute("upper", "test"))

	// Generic type implementing interface
	result := service.Ok[int](42)
	var proc service.Processor = result
	fmt.Println(proc.Process("via-interface"))

	// Cross-package generic chain + interface
	pp := service.NewPairProcessor[int, string](1, "a")
	fmt.Println(pp.Process("chain"))

	// Channel + generic pipeline
	input := make(chan int, 5)
	go func() {
		for _, n := range nums {
			input <- n
		}
		close(input)
	}()
	output := pipeline.Stage(input, func(n int) string {
		return fmt.Sprintf("item-%d", n)
	})
	results := pipeline.Collect(output)
	fmt.Println(results)
}
