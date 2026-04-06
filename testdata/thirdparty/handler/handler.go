package handler

import (
	"example.com/extlib"
	"example.com/thirdparty/store"
)

// Handle passes store.Save as a callback through extlib.Process.
// Tests the function-value callback pattern.
func Handle(data string) string {
	return extlib.Process(data, store.Save)
}

// HandleTransform passes *store.Formatter as an extlib.Transformer interface
// through extlib.RunTransform. Tests the interface dispatch pattern.
func HandleTransform(data string) string {
	return extlib.RunTransform(data, &store.Formatter{})
}

// Direct calls store.Save without going through extlib.
func Direct(data string) string {
	return store.Save(data)
}
