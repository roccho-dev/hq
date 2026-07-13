package localtool

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenericLocalToolCoreHasNoProviderNamedBranch(t *testing.T) {
	for _, directory := range []string{".", filepath.Join("..", "worker", "directexec")} {
		entries, err := os.ReadDir(directory)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(directory, entry.Name()))
			if err != nil {
				t.Fatal(err)
			}
			lower := strings.ToLower(string(data))
			for _, provider := range []string{"herdr", "codex", "claude"} {
				if strings.Contains(lower, provider) {
					t.Fatalf("generic source %s contains provider name %q", filepath.Join(directory, entry.Name()), provider)
				}
			}
		}
	}
}
