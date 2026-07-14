//go:build !windows

package workerclaim

import "hq/internal/atomicfile"

func readHeartbeatFile(path string) ([]byte, error) {
	return atomicfile.Read(path)
}
