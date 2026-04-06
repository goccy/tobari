package initlib

// Third explicit init in a separate file → SSA names it init#3.
// Closure inside it → init#3$1.
func init() {
	fn := func(s string) string { return s + "!" }
	suffix = fn("ready")
}

var suffix string

func Suffix() string {
	return suffix
}
