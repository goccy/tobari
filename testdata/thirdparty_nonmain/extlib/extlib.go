package extlib

// Process applies fn to data and returns the result.
// When data is empty, fn is not called.
func Process(data string, fn func(string) string) string {
	if data == "" {
		return ""
	}
	return fn(data)
}

// Transformer is an interface for data transformation.
type Transformer interface {
	Transform(data string) string
}

// RunTransform applies t.Transform to data.
// When data is empty, the transformer is not called.
func RunTransform(data string, t Transformer) string {
	if data == "" {
		return ""
	}
	return t.Transform(data)
}
