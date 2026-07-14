//go:build windows

package atomicfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSharedReaderAllowsConcurrentRemove(t *testing.T) {
	path := filepath.Join(t.TempDir(), "record.json")
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	reader, err := openShared(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	if err := Remove(path); err != nil {
		t.Fatalf("remove while shared reader is open: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("record still exists after remove: %v", err)
	}
}

func TestSharedReaderAllowsConcurrentReplace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "record.json")
	if err := os.WriteFile(path, []byte("{\"version\":1}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	reader, err := openShared(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	if err := WriteJSON(path, map[string]int{"version": 2}); err != nil {
		t.Fatalf("replace while shared reader is open: %v", err)
	}
	data, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), "{\"version\":2}\n"; got != want {
		t.Fatalf("record=%q want=%q", got, want)
	}
}

func TestReadSupportsExtendedLengthPath(t *testing.T) {
	path := t.TempDir()
	for index := 0; index < 6; index++ {
		path = filepath.Join(path, strings.Repeat(string(rune('a'+index)), 48))
	}
	path = filepath.Join(path, "record.json")
	if err := WriteJSON(path, map[string]int{"version": 1}); err != nil {
		t.Fatal(err)
	}
	data, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), "{\"version\":1}\n"; got != want {
		t.Fatalf("record=%q want=%q", got, want)
	}
}
