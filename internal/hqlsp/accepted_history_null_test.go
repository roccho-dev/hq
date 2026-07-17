package hqlsp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNullAcceptedInstructionIsFatal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accepted.jsonl")
	row := `{"kind":"accepted.instruction","queue":"instruction.jsonl","instruction":null}`
	if err := os.WriteFile(path, []byte(row+"\n"), 0o600); err != nil { t.Fatal(err) }
	records, report, err := (fileAcceptedHistoryReader{}).Read(path)
	if err == nil || !report.Fatal || len(records) != 0 { t.Fatalf("null instruction did not disable history: records=%#v report=%#v err=%v", records, report, err) }
}
