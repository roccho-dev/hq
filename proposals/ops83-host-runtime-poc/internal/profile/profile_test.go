package profile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadRequiresEveryProfileJSONL(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "profiles", "local"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "profiles", "local", "catalog.jsonl"), []byte(`{"kind":"hq.commandSpec.v1","id":"host.open","label":"explorer.open","detail":"open","insertText":"{}","bufferKind":"hq.hostOpenRequest.v1"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(root, "local")
	if err == nil || !strings.Contains(err.Error(), "required profile JSONL is missing") {
		t.Fatalf("expected missing queue JSONL error, got %v", err)
	}
}
