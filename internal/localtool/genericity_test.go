package localtool

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"
)

var forbiddenProviderNames = []string{"herdr", "codex", "claude", "aws", "gcp", "gcloud", "wslc", "oci"}

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
			path := filepath.Join(directory, entry.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if provider, found := providerNameInSource(string(data), forbiddenProviderNames); found {
				t.Fatalf("generic source %s contains provider name %q", path, provider)
			}
		}
	}
}

func TestWorldDataProviderGuardRecognizesIdentifierAndLiteralForms(t *testing.T) {
	tests := []struct {
		name, source, want string
	}{
		{name: "plain literal", source: `const target = "aws"`, want: "aws"},
		{name: "underscore boundary", source: "var provider_aws string", want: "aws"},
		{name: "acronym camel", source: "var AWSClient string", want: "aws"},
		{name: "compound provider camel", source: "var GCloudClient string", want: "gcloud"},
		{name: "lower camel", source: "var gcloudClient string", want: "gcloud"},
		{name: "punctuation boundary", source: `const endpoint = "aws.s3"`, want: "aws"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, found := providerNameInSource(test.source, forbiddenProviderNames)
			if !found || got != test.want {
				t.Fatalf("providerNameInSource(%q)=(%q,%v), want (%q,true)", test.source, got, found, test.want)
			}
		})
	}
}

func TestWorldDataProviderGuardDoesNotRecombineAcrossBoundaries(t *testing.T) {
	for _, source := range []string{
		"open a ws connection",
		"open a_ws connection",
		"open a.ws connection",
		"use g cloud login",
		"use g_cloud login",
	} {
		if provider, found := providerNameInSource(source, forbiddenProviderNames); found {
			t.Fatalf("source %q false-positive provider %q", source, provider)
		}
	}
}

func providerNameInSource(source string, providers []string) (string, bool) {
	for _, run := range alphanumericRuns(source) {
		fragments := identifierFragments(run)
		for start := range fragments {
			combined := ""
			for end := start; end < len(fragments); end++ {
				combined += strings.ToLower(fragments[end])
				for _, provider := range providers {
					if combined == provider {
						return provider, true
					}
				}
			}
		}
	}
	return "", false
}

func alphanumericRuns(source string) []string {
	var runs []string
	var current []rune
	flush := func() {
		if len(current) != 0 {
			runs = append(runs, string(current))
			current = nil
		}
	}
	for _, character := range source {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			current = append(current, character)
			continue
		}
		flush()
	}
	flush()
	return runs
}

func identifierFragments(run string) []string {
	runes := []rune(run)
	if len(runes) == 0 {
		return nil
	}
	start := 0
	fragments := make([]string, 0, 4)
	for index := 1; index < len(runes); index++ {
		previous := runes[index-1]
		current := runes[index]
		nextLower := index+1 < len(runes) && unicode.IsLower(runes[index+1])
		boundary := unicode.IsUpper(current) && (unicode.IsLower(previous) || unicode.IsDigit(previous) || unicode.IsUpper(previous) && nextLower)
		if boundary {
			fragments = append(fragments, string(runes[start:index]))
			start = index
		}
	}
	fragments = append(fragments, string(runes[start:]))
	return fragments
}
