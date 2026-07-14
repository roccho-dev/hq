//go:build windows

package workerclaim

import (
	"os"
	"time"

	"hq/internal/atomicfile"
)

func readHeartbeatFile(path string) ([]byte, error) {
	deadline := time.Now().Add(250 * time.Millisecond)
	for {
		data, err := atomicfile.Read(path)
		if err == nil || !os.IsNotExist(err) || !time.Now().Before(deadline) {
			return data, err
		}
		time.Sleep(time.Millisecond)
	}
}
