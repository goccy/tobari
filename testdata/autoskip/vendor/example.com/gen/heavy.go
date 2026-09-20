package gen

// heavy is replaced by the test with a large generated call graph, so that
// the package takes a noticeable share of the analysis.
func heavy(n int) int {
	return n
}
