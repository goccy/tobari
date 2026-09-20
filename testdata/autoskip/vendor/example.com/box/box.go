// Package box stands for a dependency that never calls the caller's code but
// carries its values from one place to another.
package box

var content any

// Put stores v.
func Put(v any) {
	content = v
}

// Get returns the stored value.
func Get() any {
	return content
}
