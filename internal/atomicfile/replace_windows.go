//go:build windows

package atomicfile

import (
	"os"
	"time"
)

func remove(path string) error {
	deadline := time.Now().Add(250 * time.Millisecond)
	for {
		err := os.Remove(path)
		if err == nil || !transientReplaceError(err) {
			return err
		}
		if !time.Now().Before(deadline) {
			return err
		}
		time.Sleep(time.Millisecond)
	}
}
