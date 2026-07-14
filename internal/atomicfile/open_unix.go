//go:build !windows

// Package atomicfile supplies read handles compatible with atomic file
// replacement on each supported host.
package atomicfile

import "os"

func OpenRead(path string) (*os.File, error) {
	return os.Open(path)
}

func Replace(temporary, destination string) error {
	return os.Rename(temporary, destination)
}
