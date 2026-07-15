package hqlsp

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
