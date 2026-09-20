package utils

import (
	"bytes"
	"os"
	"path/filepath"
)

// AtomicWriteFile writes content to path atomically using a temp file and rename.
// If the file already exists with the same content, it's a no-op.
func AtomicWriteFile(path string, content []byte) error {
	// Check if file already exists with correct content
	existing, err := os.ReadFile(path)
	if err == nil && bytes.Equal(existing, content) {
		return nil
	}

	// Write to temp file in the same directory (for same-filesystem rename)
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }() // Clean up on error

	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	// Atomic rename
	return os.Rename(tmpPath, path)
}
