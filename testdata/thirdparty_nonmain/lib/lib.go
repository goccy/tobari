package lib

import (
	"example.com/extlib2"
	"example.com/thirdparty_nonmain/store"
)

// Handle passes store.Save as a callback through extlib.Process.
func Handle(data string) string {
	return extlib.Process(data, store.Save)
}

// HandleTransform passes *store.Formatter as an extlib.Transformer interface.
func HandleTransform(data string) string {
	return extlib.RunTransform(data, &store.Formatter{})
}

// Direct calls store.Save without going through extlib.
func Direct(data string) string {
	return store.Save(data)
}
