package worker

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFollowRunFailsClosedWhenEmittedEvidenceChanges(t *testing.T) {
	cases := []struct {
		name     string
		wantCode string
		mutate   func([]ResultRow) []ResultRow
	}{
		{
			name:     "truncated",
			wantCode: "run_evidence_truncated",
			mutate: func(rows []ResultRow) []ResultRow {
				return append([]ResultRow(nil), rows[:1]...)
			},
		},
		{
			name:     "changed",
			wantCode: "run_evidence_changed",
			mutate: func(rows []ResultRow) []ResultRow {
				changed := append([]ResultRow(nil), rows...)
				changed[1].EventID = "changed-event-id"
				return changed
			},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "events.jsonl")
			at := time.Date(2026, 7, 10, 10, 0, 0, 0, time.UTC)
			rows := []ResultRow{
				{EventID: "e0", Version: ResultVersionV1, RunID: "run-append-only", InstructionID: "ins-append-only", Target: "codex", Kind: ResultAccepted, Seq: 0, RecordedAt: at},
				{EventID: "e1", Version: ResultVersionV1, RunID: "run-append-only", InstructionID: "ins-append-only", Target: "codex", Kind: ResultStarted, Seq: 1, RecordedAt: at.Add(time.Second)},
			}
			if err := writeResultRows(path, rows); err != nil {
				t.Fatal(err)
			}

			mutated := false
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			err := FollowRun(ctx, path, "run-append-only", true, time.Millisecond, func(row ResultRow) error {
				if row.Seq == 1 && !mutated {
					mutated = true
					return writeResultRows(path, testCase.mutate(rows))
				}
				return nil
			})
			var observationError *ObservationError
			if !errors.As(err, &observationError) || observationError.Code != testCase.wantCode {
				t.Fatalf("err=%T %v want code %q", err, err, testCase.wantCode)
			}
		})
	}
}

func writeResultRows(path string, rows []ResultRow) error {
	data := make([]byte, 0)
	for _, row := range rows {
		encoded, err := json.Marshal(row)
		if err != nil {
			return err
		}
		data = append(data, encoded...)
		data = append(data, '\n')
	}
	return os.WriteFile(path, data, 0o600)
}
