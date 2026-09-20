// Package gen stands for a large generated dependency, such as a protobuf
// client: a lot of code that only ever handles plain data — unless Apply is
// used, which calls back whatever it is handed.
package gen

// Sum computes a value from n without involving the caller's code.
func Sum(n int) int {
	return heavy(n)
}

// Apply calls fn with n.
func Apply(n int, fn func(int) int) int {
	return fn(n)
}
