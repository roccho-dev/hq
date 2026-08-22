package worker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"
)

// TestTerminalRecoveryLineageE2E fixes only the terminal-recovery sequence.
// It does not execute a provider and does not repeat dispatch/lifecycle E2Es:
// a failed, timeout, or cancelled attempt remains immutable while a new
// instruction with reply_to receives its own run and terminal evidence.
func TestTerminalRecoveryLineageE2E(t *testing.T) {
	for _, terminal := range []string{"failed", "timeout", "cancelled"} {
		t.Run(terminal, func(t *testing.T) {
			priorID := "instruction-prior-" + terminal
			retryID := "instruction-retry-" + terminal
			priorRun := "run-prior-" + terminal
			retryRun := "run-retry-" + terminal
			priorRaw := []byte(fmt.Sprintf(`{"id":%q,"version":"instruction.v1","op":"run","target":"sh","payload":{"argv":["printf","prior"]},"created_at":"2026-08-22T00:00:00Z"}`, priorID))
			retryRaw := []byte(fmt.Sprintf(`{"id":%q,"version":"instruction.v1","op":"run","target":"sh","payload":{"argv":["printf","retry"]},"created_at":"2026-08-22T00:01:00Z","reply_to":%q}`, retryID, priorID))
			priorSnapshot := append([]byte(nil), priorRaw...)

			instructions := []Instruction{
				decodeRecoveryInstruction(t, priorRaw),
				decodeRecoveryInstruction(t, retryRaw),
			}
			results := []ResultRow{
				decodeRecoveryResult(t, resultJSON(priorRun, priorID, "accepted", 1, "2026-08-22T00:00:01Z", "")),
				decodeRecoveryResult(t, resultJSON(priorRun, priorID, "started", 2, "2026-08-22T00:00:02Z", "")),
				decodeRecoveryResult(t, resultJSON(priorRun, priorID, terminal, 3, "2026-08-22T00:00:03Z", `,"error":{"code":"terminal","message":"terminal attempt","retryable":true}`)),
				decodeRecoveryResult(t, resultJSON(retryRun, retryID, "accepted", 1, "2026-08-22T00:01:01Z", "")),
				decodeRecoveryResult(t, resultJSON(retryRun, retryID, "started", 2, "2026-08-22T00:01:02Z", "")),
				decodeRecoveryResult(t, resultJSON(retryRun, retryID, "completed", 3, "2026-08-22T00:01:03Z", `,"final":{"text":"recovered"}`)),
			}

			prior, priorDiagnostics, err := BuildRunDetail(priorRun, instructions, results)
			if err != nil || len(priorDiagnostics) != 0 {
				t.Fatalf("prior detail: err=%v diagnostics=%#v", err, priorDiagnostics)
			}
			retry, retryDiagnostics, err := BuildRunDetail(retryRun, instructions, results)
			if err != nil || len(retryDiagnostics) != 0 {
				t.Fatalf("retry detail: err=%v diagnostics=%#v", err, retryDiagnostics)
			}
			if prior.Run.Status != terminal || prior.Run.InstructionID != priorID {
				t.Fatalf("prior attempt changed: %#v", prior.Run)
			}
			if retry.Run.Status != "completed" || retry.Run.InstructionID != retryID {
				t.Fatalf("retry attempt is not independent: %#v", retry.Run)
			}
			ledger, diagnostics := BuildLedger(instructions, results)
			if len(diagnostics) != 0 || len(ledger) != 2 {
				t.Fatalf("ledger=%#v diagnostics=%#v", ledger, diagnostics)
			}
			if !bytes.Equal(priorRaw, priorSnapshot) {
				t.Fatalf("prior instruction bytes were mutated")
			}
			var retryObject map[string]any
			if err := json.Unmarshal(retryRaw, &retryObject); err != nil {
				t.Fatal(err)
			}
			if retryObject["reply_to"] != priorID || retryObject["id"] == priorID {
				t.Fatalf("retry lineage=%#v", retryObject)
			}
		})
	}
}

func decodeRecoveryInstruction(t *testing.T, raw []byte) Instruction {
	t.Helper()
	var value Instruction
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func decodeRecoveryResult(t *testing.T, raw string) ResultRow {
	t.Helper()
	var value ResultRow
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		t.Fatalf("decode result: %v\n%s", err, raw)
	}
	return value
}

func resultJSON(runID, instructionID, kind string, seq int, recordedAt, extra string) string {
	return fmt.Sprintf(`{"event_id":"event-%s-%d","version":"result.v1","run_id":%q,"instruction_id":%q,"target":"sh","kind":%q,"seq":%d,"recorded_at":%q%s}`,
		runID, seq, runID, instructionID, kind, seq, recordedAt, extra)
}
