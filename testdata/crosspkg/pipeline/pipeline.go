package pipeline

// Stage creates a pipeline stage that transforms input values via fn.
func Stage[In, Out any](input <-chan In, fn func(In) Out) <-chan Out {
	output := make(chan Out)
	go func() {
		defer close(output)
		for v := range input {
			output <- fn(v)
		}
	}()
	return output
}

// Collect gathers all values from a channel into a slice.
func Collect[T any](ch <-chan T) []T {
	var result []T
	for v := range ch {
		result = append(result, v)
	}
	return result
}
