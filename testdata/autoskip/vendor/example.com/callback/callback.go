// Package callback stands for a dependency that calls the caller's code back.
package callback

// Each calls fn for every item.
func Each(items []int, fn func(int)) {
	for _, item := range items {
		fn(item)
	}
}
