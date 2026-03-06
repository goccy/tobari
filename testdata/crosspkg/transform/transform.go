package transform

// Map applies fn to each element of items.
func Map[T, U any](items []T, fn func(T) U) []U {
	result := make([]U, len(items))
	for i, item := range items {
		result[i] = fn(item)
	}
	return result
}

// Filter returns elements that satisfy pred.
func Filter[T any](items []T, pred func(T) bool) []T {
	var result []T
	for _, item := range items {
		if pred(item) {
			result = append(result, item)
		}
	}
	return result
}

// Reduce folds items into a single value.
func Reduce[T, U any](items []T, initial U, fn func(U, T) U) U {
	acc := initial
	for _, item := range items {
		acc = fn(acc, item)
	}
	return acc
}

// Compose composes two functions.
func Compose[A, B, C any](f func(A) B, g func(B) C) func(A) C {
	return func(a A) C {
		return g(f(a))
	}
}
