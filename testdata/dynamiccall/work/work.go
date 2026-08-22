package work

import "sync"

var (
	once   sync.Once
	config string
)

// Do lazily initializes config through sync.Once. The Once invokes its
// stored function via a dynamic function-value call inside the standard
// library, which a signature-based call-graph resolution would connect to
// every address-taken func() in the binary.
func Do() string {
	once.Do(initConfig)
	return config
}

// initConfig actually flows into the Once, so it is reachable from Do.
func initConfig() {
	config = "initialized"
}

// Extra never flows into the Once. Its address is taken below, giving it
// the same shape as the stored function, but it is not reachable from Do.
func Extra() {
	config = "extra"
}

// handlers takes Extra's address so that it is an address-taken func() —
// the shape that a purely signature-based resolution would treat as a
// callee of any dynamic func() call.
var handlers = []func(){Extra}

// RunHandlers is the real entry point for the handlers; only tests that
// call it reach Extra.
func RunHandlers() {
	for _, h := range handlers {
		h()
	}
}
