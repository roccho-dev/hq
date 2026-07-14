// Package atomicfile adapts small machine-readable worker state to filesystem
// replacement without exposing partially written records to concurrent readers.
package atomicfile

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// WriteJSON replaces path with one complete JSON record.
func WriteJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".hq-atomic-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	encoder := json.NewEncoder(temporary)
	encoder.SetEscapeHTML(false)
	writeErr := encoder.Encode(value)
	if writeErr == nil {
		writeErr = temporary.Sync()
	}
	closeErr := temporary.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	return replace(temporaryPath, path)
}

// Read reads a record while allowing the writer to replace or remove it.
func Read(path string) ([]byte, error) {
	return read(path)
}

// Remove deletes a record, retrying transient platform sharing conflicts.
func Remove(path string) error {
	return remove(path)
}
