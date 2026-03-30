package chprovider

// NewChan creates and returns a new unbuffered channel.
// The channel type comes from this external package, so the cover phase's
// stubImporter cannot resolve it — requiring compile-phase resolution.
func NewChan() chan int {
	return make(chan int)
}
