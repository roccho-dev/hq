package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCoreHasNoConcreteExternalSchemaTerms(t *testing.T) {
	for _, term := range []string{
		"ADRS projected",
		"adrs.projected",
		"DefaultSchemaJSONL",
		"queue envelope",
		"source_ref",
	} {
		assertCoreSourceDoesNotContain(t, term)
	}
}

func assertCoreSourceDoesNotContain(t *testing.T, term string) {
	t.Helper()
	matches, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range matches {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), term) {
			t.Fatalf("core file %s contains concrete schema term %q", path, term)
		}
	}
}
