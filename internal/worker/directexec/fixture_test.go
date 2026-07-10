package directexec

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
)

type fixtureCase struct {
	ID                       string `json:"id"`
	Stage                    string `json:"stage"`
	ExpectedTerminal         string `json:"expected_terminal"`
	ExpectedError            string `json:"expected_error,omitempty"`
	ExpectedProcessStarts    *int   `json:"expected_process_starts,omitempty"`
	ExpectedAdditionalStarts *int   `json:"expected_additional_process_starts,omitempty"`
}

func TestFixtureMatrixCoversGDestructiveCases(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source")
	}
	path := filepath.Join(filepath.Dir(file), "../../../spec/fixtures/directexec/cases.jsonl")
	input, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()

	seen := map[string]fixtureCase{}
	scanner := bufio.NewScanner(input)
	for scanner.Scan() {
		var row fixtureCase
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			t.Fatalf("decode fixture: %v", err)
		}
		if row.ID == "" || row.Stage == "" || row.ExpectedTerminal == "" {
			t.Fatalf("incomplete fixture: %+v", row)
		}
		if _, exists := seen[row.ID]; exists {
			t.Fatalf("duplicate fixture id %q", row.ID)
		}
		seen[row.ID] = row
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}

	want := []string{
		"approved_exact_path", "bare_name_path_lookup", "cwd_escape", "deadline", "explicit_cancel",
		"metacharacters_literal", "missing_approval", "nonzero_exit", "normal_duplicate", "stale_approval", "unknown_target",
	}
	got := make([]string, 0, len(seen))
	for id := range seen {
		got = append(got, id)
	}
	sort.Strings(got)
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("fixture ids=%v want=%v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("fixture ids=%v want=%v", got, want)
		}
	}
}
