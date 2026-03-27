package main

import (
	"fmt"
	"testing"

	"example.com/crosspkg/container"
	"example.com/crosspkg/pipeline"
	"example.com/crosspkg/service"
	"example.com/crosspkg/transform"
)

func TestGenericContainer(t *testing.T) {
	pair := container.NewPair(42, "hello")
	swapped := pair.Swap()
	if swapped.First != "hello" || swapped.Second != 42 {
		t.Fatal("unexpected swap result")
	}

	stack := &container.Stack[container.Pair[int, string]]{}

	// Pop on empty stack returns false.
	_, ok := stack.Pop()
	if ok {
		t.Fatal("expected false for empty stack pop")
	}

	stack.Push(pair)
	if stack.Len() != 1 {
		t.Fatal("expected stack length 1")
	}

	// Pop on non-empty stack returns the item.
	item, ok := stack.Pop()
	if !ok || item.First != 42 {
		t.Fatal("expected successful pop")
	}
}

func TestTransformPipeline(t *testing.T) {
	nums := []int{1, 2, 3, 4, 5}
	doubled := transform.Map(nums, func(n int) int { return n * 2 })
	if doubled[0] != 2 {
		t.Fatal("expected 2")
	}
	evens := transform.Filter(nums, func(n int) bool { return n%2 == 0 })
	if len(evens) != 2 {
		t.Fatal("expected 2 evens")
	}
	sum := transform.Reduce(nums, 0, func(acc, n int) int { return acc + n })
	if sum != 15 {
		t.Fatal("expected sum 15")
	}
}

func TestFunctionComposition(t *testing.T) {
	double := func(n int) int { return n * 2 }
	toString := func(n int) string { return fmt.Sprintf("%d", n) }
	composed := transform.Compose(double, toString)
	if composed(5) != "10" {
		t.Fatal("expected '10'")
	}
}

func TestInterfaceWithGeneric(t *testing.T) {
	result := service.Ok[int](42)
	var proc service.Processor = result
	out := proc.Process("test")
	if out != "processed:test" {
		t.Fatal("unexpected result:", out)
	}

	reg := service.NewRegistry[UpperProcessor]()

	// Execute with unregistered key returns empty string.
	if reg.Execute("missing", "input") != "" {
		t.Fatal("expected empty string for missing key")
	}

	reg.Register("up", UpperProcessor{})
	if reg.Execute("up", "hello") != "UPPER:hello" {
		t.Fatal("registry failed")
	}

	// ConditionalProcess with run=false: StringProcessor.Process is never called,
	// but should appear in coverprofile via deps.
	service.ConditionalProcess(transform.StringProcessor{Prefix: "unused"}, "test", false)
}

func TestCrossPackageGenericChain(t *testing.T) {
	pp := service.NewPairProcessor[int, string](1, "a")
	// Use ConditionalProcess with run=false:
	// PairProcessor.Process is never called, but should appear in deps.
	// PairProcessor.Process internally calls container.Pair.Swap,
	// so Swap should also appear via transitive deps.
	service.ConditionalProcess(pp, "chain", false)
}

func TestChannelGenericPipeline(t *testing.T) {
	input := make(chan int, 3)
	go func() {
		input <- 1
		input <- 2
		input <- 3
		close(input)
	}()
	output := pipeline.Stage(input, func(n int) string {
		return fmt.Sprintf("v%d", n)
	})
	results := pipeline.Collect(output)
	if len(results) != 3 {
		t.Fatal("expected 3 results")
	}
}
