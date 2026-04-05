package store

func Save(data string) string {
	return "saved:" + data
}

// Formatter implements extlib.Transformer via interface dispatch.
type Formatter struct{}

func (f *Formatter) Transform(data string) string {
	return "formatted:" + data
}
