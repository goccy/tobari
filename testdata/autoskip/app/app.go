package app

import (
	"example.com/box"
	"example.com/callback"
	"example.com/gen"
)

// Compute only passes plain data to gen.
func Compute(n int) int {
	return helper(gen.Sum(n))
}

func helper(n int) int {
	return n + 1
}

// Process hands callback a function, which callback calls back.
func Process(items []int) int {
	total := 0
	callback.Each(items, func(v int) {
		total += visit(v)
	})
	return total
}

func visit(v int) int {
	return v * 2
}

// Runner is implemented by Task.
type Runner interface {
	Run() int
}

// Task is kept in box between Keep and Resume.
type Task struct {
	N int
}

// Run does the task's work.
func (t Task) Run() int {
	return work(t.N)
}

func work(n int) int {
	return n * 3
}

// Keep puts a Task into box.
func Keep(n int) {
	box.Put(Task{N: n})
}

// Resume takes whatever is in box out and runs it. That it is a Task is only
// visible by following the value through box.
func Resume() int {
	r, ok := box.Get().(Runner)
	if !ok {
		return 0
	}
	return r.Run()
}
