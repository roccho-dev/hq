package worker

import (
	"encoding/json"
	"testing"
)

func TestAgentPayloadContractsAcceptEveryAction(t *testing.T) {
	cases := []struct {
		target  string
		payload string
	}{
		{"herdr", `{"action":"start","cwd":".","name":"audit","no_focus":true,"split":"right","command_argv":["claude","-p"],"prompt":"review"}`},
		{"herdr", `{"action":"read","agent":"review","source":"recent-unwrapped","lines":50}`},
		{"herdr", `{"action":"observe","agent":"review","wait_status":"idle","timeout_ms":1000}`},
		{"herdr", `{"action":"attach","agent":"review","takeover":true}`},
		{"codex", `{"action":"exec","prompt":"review","cwd":".","sandbox":"workspace-write","skip_git_repo_check":true}`},
		{"codex", `{"action":"resume","prompt":"continue","session_id":"thread-1","output_path":".hq/final/answer.txt"}`},
		{"claude", `{"action":"print","prompt":"review","output_format":"json","max_turns":3,"bare":true}`},
		{"claude", `{"action":"resume","prompt":"continue","session_id":"session-1","output_format":"stream-json"}`},
		{"claude", `{"action":"background","prompt":"work","name":"audit"}`},
		{"claude", `{"action":"logs","session_id":"session-1"}`},
		{"claude", `{"action":"attach","session_id":"session-1"}`},
	}
	contract := DefaultContract()
	for index, testCase := range cases {
		row := ReadRow{Instruction: Instruction{
			ID: "i", Version: InstructionVersionV1, Op: "run", Target: testCase.target,
			Payload: json.RawMessage(testCase.payload), CreatedAt: "2026-07-10T00:00:00Z",
		}}
		if diagnostics := contract.Validate(row); len(diagnostics) != 0 {
			t.Fatalf("case %d target=%s diagnostics=%+v", index, testCase.target, diagnostics)
		}
	}
}

func TestAgentPayloadContractsFailClosed(t *testing.T) {
	cases := []struct {
		target  string
		payload string
	}{
		{"herdr", `{"action":"start","prompt":"review"}`},
		{"herdr", `{"action":"read","agent":"review","source":"guess"}`},
		{"herdr", `{"action":"attach","agent":"review","prompt":"hidden"}`},
		{"codex", `{"action":"resume","prompt":"continue"}`},
		{"codex", `{"action":"exec","prompt":"review","session_id":"hidden"}`},
		{"codex", `{"action":"exec","prompt":"review","sandbox":"unbounded"}`},
		{"claude", `{"action":"background","prompt":"work","output_format":"json"}`},
		{"claude", `{"action":"logs","session_id":"session-1","prompt":"hidden"}`},
		{"claude", `{"action":"print","prompt":"review","max_turns":0}`},
		{"claude", `{"action":"print","prompt":"review","hidden":true}`},
	}
	contract := DefaultContract()
	for index, testCase := range cases {
		row := ReadRow{Instruction: Instruction{
			ID: "i", Version: InstructionVersionV1, Op: "run", Target: testCase.target,
			Payload: json.RawMessage(testCase.payload), CreatedAt: "2026-07-10T00:00:00Z",
		}}
		diagnostics := contract.Validate(row)
		if len(diagnostics) == 0 || diagnostics[0].Code != "invalid_payload" {
			t.Fatalf("case %d target=%s diagnostics=%+v", index, testCase.target, diagnostics)
		}
	}
}
