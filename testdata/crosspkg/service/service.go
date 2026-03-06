package service

import "example.com/crosspkg/container"

// Processor defines a string processing interface.
type Processor interface {
	Process(input string) string
}

// Registry maps names to processors.
type Registry[T Processor] struct {
	processors map[string]T
}

func NewRegistry[T Processor]() *Registry[T] {
	return &Registry[T]{processors: make(map[string]T)}
}

func (r *Registry[T]) Register(name string, p T) {
	r.processors[name] = p
}

func (r *Registry[T]) Execute(name string, input string) string {
	p, ok := r.processors[name]
	if !ok {
		return ""
	}
	return p.Process(input)
}

// Result is a generic type that implements Processor.
type Result[T any] struct {
	value T
	err   error
}

func Ok[T any](v T) Result[T] {
	return Result[T]{value: v}
}

func (r Result[T]) Process(input string) string {
	return "processed:" + input
}

// PairProcessor uses container.Pair internally and implements Processor.
type PairProcessor[A, B any] struct {
	pair container.Pair[A, B]
}

func NewPairProcessor[A, B any](a A, b B) *PairProcessor[A, B] {
	return &PairProcessor[A, B]{
		pair: container.NewPair(a, b),
	}
}

func (p *PairProcessor[A, B]) Process(input string) string {
	_ = p.pair.Swap()
	return "pair:" + input
}
