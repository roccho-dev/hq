//go:build !windows

package atomicfile

import "os"

func remove(path string) error {
	return os.Remove(path)
}
