package hqlsp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hq/internal/core"
	"hq/internal/hq"
	"hq/internal/hqprofile"
)

const acceptedHistoryWorld = `{"kind":"hq.world.v1","world_id":"history.lsp"}
{"kind":"hq.command.v1","command_id":"demo","command_version":"1","name":"demo","instruction":{"version":"instruction.v1","op":"run","target":"demo","payload":{}},"fields":[{"name":"value","type":"string","required":true,"history_policy":"search","bind":"payload.value"}]}`

func writeAcceptedHistoryFixture(t *testing.T, path string, world *hq.JsonlWorld) {
	t.Helper()
	draft, _, err := hq.CompileSelectedCommandObjectWithRange("@demo\nvalue=alpha", 1, world)
	if err != nil {
		t.Fatal(err)
	}
	draft.Instruction["id"] = "ins-history-001"
	draft.Instruction["created_at"] = "2026-07-16T00:00:00Z"
	if err := hq.BindFinalInstructionProvenance(&draft, world, hq.CommandInputKind); err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	if err := (hq.QueueWriter{W: &encoded}).Append(draft); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestAcceptedHistoryLoadsOnceAndCorruptionDegradesToWorldOnly(t *testing.T) {
	root := t.TempDir()
	worldPath := filepath.Join(root, "world.jsonl")
	acceptedPath := filepath.Join(root, "accepted.jsonl")
	if err := os.WriteFile(worldPath, []byte(acceptedHistoryWorld), 0o600); err != nil {
		t.Fatal(err)
	}
	world, err := hq.LoadSelectedWorldJSONL(strings.NewReader(acceptedHistoryWorld))
	if err != nil {
		t.Fatal(err)
	}
	writeAcceptedHistoryFixture(t, acceptedPath, world)
	worldOnly, err := hq.PrepareWorldRecall(world)
	if err != nil {
		t.Fatal(err)
	}
	profile := hqprofile.Profile{Name: "history", DeploymentID: "dep-history", WorldPath: worldPath, AcceptedPath: acceptedPath}
	server, err := NewWithAcceptedHistory(profile)
	if err != nil {
		t.Fatal(err)
	}
	if server.recall == nil || server.recall.ID() == worldOnly.ID() || server.HistoryReport().Fatal {
		t.Fatalf("history was not composed: index=%v report=%#v", server.recall, server.HistoryReport())
	}
	const uri = "file:///empty.hq"
	server.documents[uri] = document{Text: "", Version: 1}
	before, err := os.ReadFile(acceptedPath)
	if err != nil {
		t.Fatal(err)
	}
	var response bytes.Buffer
	request := message{JSONRPC: "2.0", ID: json.RawMessage(`1`), Params: mustJSON(map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     map[string]any{"line": 0, "character": 0},
	})}
	if err := server.complete(&response, request); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(acceptedPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("completion changed accepted history: err=%v", err)
	}
	wire := decodeCompletionResponse(t, response.String())
	if len(wire.Result.Items) != 1 || wire.Result.Items[0].TextEdit.NewText != "@demo\nvalue=alpha" {
		t.Fatalf("empty-draft recall=%#v", wire.Result)
	}
	if err := os.WriteFile(acceptedPath, append(before, []byte("{\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	degraded, err := NewWithAcceptedHistory(profile)
	if err != nil {
		t.Fatal(err)
	}
	if !degraded.HistoryReport().Fatal || degraded.recall.ID() != worldOnly.ID() {
		t.Fatalf("corruption did not degrade exactly: index=%q world=%q report=%#v", degraded.recall.ID(), worldOnly.ID(), degraded.HistoryReport())
	}
}

func TestExplicitSubmitAppendsOneRowWithAcceptedInput(t *testing.T) {
	world, err := hq.LoadSelectedWorldJSONL(strings.NewReader(acceptedHistoryWorld))
	if err != nil {
		t.Fatal(err)
	}
	acceptedPath := filepath.Join(t.TempDir(), "accepted.jsonl")
	if err := os.WriteFile(acceptedPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	const uri = "file:///submit.hq"
	server := &Server{
		profile: hqprofile.Profile{Name: "history", DeploymentID: "dep-history", AcceptedPath: acceptedPath},
		world: world,
		documents: map[string]document{uri: {Text: "@demo\nvalue=alpha", Version: 1}},
	}
	var output bytes.Buffer
	request := message{JSONRPC: "2.0", ID: json.RawMessage(`1`), Params: mustJSON(map[string]any{
		"command": "hq.submit",
		"arguments": []any{map[string]any{"uri": uri, "version": 1, "line": 1}},
	})}
	if err := server.executeCommand(&output, request); err != nil {
		t.Fatal(err)
	}
	rows := readQueueRows(t, acceptedPath)
	if len(rows) != 1 || rows[0]["accepted_input"] == nil {
		t.Fatalf("accepted rows=%#v", rows)
	}
	input := rows[0]["accepted_input"].(map[string]any)
	if input["kind"] != "hq.accepted-input.v1" || input["accepted_input_digest"] == "" {
		t.Fatalf("accepted input=%#v", input)
	}
}

func TestAcceptedHistoryBoundsNewestRowsBeforeEligibility(t *testing.T) {
	world, err := hq.LoadSelectedWorldJSONL(strings.NewReader(acceptedHistoryWorld))
	if err != nil {
		t.Fatal(err)
	}
	acceptedPath := filepath.Join(t.TempDir(), "accepted.jsonl")
	writeAcceptedHistoryFixture(t, acceptedPath, world)
	file, err := os.OpenFile(acceptedPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 7, 17, 0, 0, 0, 0, time.UTC)
	for index := 0; index < 1000; index++ {
		created := start.Add(time.Duration(index) * time.Second).Format(time.RFC3339Nano)
		row := fmt.Sprintf(`{"kind":"accepted.instruction","queue":"instruction.jsonl","instruction":{"id":"legacy-%04d","version":"instruction.v1","op":"run","target":"demo","payload":{},"created_at":%q}}`, index, created)
		if _, err := fmt.Fprintln(file, row); err != nil {
			_ = file.Close()
			t.Fatal(err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	records, report, err := (fileAcceptedHistoryReader{}).Read(acceptedPath)
	if err != nil || report.Fatal {
		t.Fatalf("read failed: records=%d report=%#v err=%v", len(records), report, err)
	}
	if len(records) != 0 || historyFindingCount(report, "legacy-row") != 1000 {
		t.Fatalf("older eligible history escaped newest-row bound: records=%#v report=%#v", records, report)
	}
}

func TestMalformedLegacyInstructionIsFatal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accepted.jsonl")
	row := `{"kind":"accepted.instruction","queue":"instruction.jsonl","instruction":"not-an-object"}`
	if err := os.WriteFile(path, []byte(row+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	records, report, err := (fileAcceptedHistoryReader{}).Read(path)
	if err == nil || !report.Fatal || len(records) != 0 {
		t.Fatalf("malformed legacy row did not disable history: records=%#v report=%#v err=%v", records, report, err)
	}
}

func historyFindingCount(report core.AcceptedHistoryReport, code string) int {
	for _, finding := range report.Findings {
		if finding.Code == code {
			return finding.Count
		}
	}
	return 0
}
