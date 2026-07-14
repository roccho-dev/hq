//go:build linux

package selectedworld

import (
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestLoadRejectsFIFOWithoutBlocking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "world.fifo")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := Load(path)
		result <- err
	}()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("FIFO selected-world path was accepted")
		}
	case <-time.After(time.Second):
		t.Fatal("FIFO rejection blocked in open")
	}
}
