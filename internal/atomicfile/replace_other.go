//go:build !windows

package atomicfile

import "os"

func replace(oldPath, newPath string) error {
	return os.Rename(oldPath, newPath)
}

func read(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func remove(path string) error {
	return os.Remove(path)
}
