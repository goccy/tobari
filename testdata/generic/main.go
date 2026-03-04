package main

import "fmt"

type Result[T any] struct {
	value T
	err   error
}

func Ok[T any](value T) Result[T] {
	return Result[T]{value: value}
}

func Err[T any](err error) Result[T] {
	return Result[T]{err: err}
}

func (r Result[T]) IsOk() bool {
	return r.err == nil
}

func (r Result[T]) Unwrap() T {
	if r.err != nil {
		panic(r.err)
	}
	return r.value
}

func (r Result[T]) UnwrapOr(def T) T {
	if r.err != nil {
		return def
	}
	return r.value
}

func main() {
	r := Ok(42)
	fmt.Println(r.IsOk())
	fmt.Println(r.Unwrap())

	e := Err[string](fmt.Errorf("something went wrong"))
	fmt.Println(e.IsOk())
	fmt.Println(e.UnwrapOr("fallback"))
}
