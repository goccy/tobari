package initlib

// Package-level variable closures (belong to SSA's synthetic init).
var (
	transform = func(s string) string { return "[" + s + "]" }
	format    = func(s string) string { return "(" + s + ")" }
)

// First explicit init → SSA names it init#1.
// Closures inside it → init#1$1, init#1$2.
func init() {
	fn1 := func(x int) int { return x + 1 }
	fn2 := func(x int) int { return x * 2 }
	_ = fn1(0)
	_ = fn2(0)
}

// Second explicit init → SSA names it init#2.
// Closure inside it → init#2$1, with nested closure → init#2$1$1.
func init() {
	outer := func(x int) int {
		inner := func(y int) int { return y + x }
		return inner(x)
	}
	_ = outer(0)
}

func Transform(s string) string {
	return transform(s)
}

func Format(s string) string {
	return format(s)
}
