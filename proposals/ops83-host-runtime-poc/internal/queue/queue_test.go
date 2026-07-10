package queue

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadRowsRejectsArbitraryExecutable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.jsonl")
	row := `{"kind":"hq.hostCommandQueued.v1","id":"bad","status":"queued","command":"cmd.exe","path":"."}` + "\n"
	if err := os.WriteFile(path, []byte(row), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadRows(path); err == nil {
		t.Fatal("arbitrary executable queue row must be rejected")
	}
}
