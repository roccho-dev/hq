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

func TestCompletionAndStaleSubmitAppendNothingThenRepeatedExplicitSubmitsAreDistinct(t *testing.T) {
	root := t.TempDir()
	queue := filepath.Join(root, "accepted.jsonl")
	if err := os.WriteFile(queue, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	uri := "file:///proof.json"
	text := `{"id":"source-id","version":"instruction.v1","op":"run","target":"sh","payload":{"argv":["printf","hello"],"cwd":"."},"created_at":"2026-07-10T07:00:00Z"}`
	server := &Server{
		profile:   hqprofile.Profile{Name: "local", DeploymentID: "dep-1", AcceptedPath: queue},
		world:     hq.DefaultWorld(),
		documents: map[string]document{uri: {Text: text, Version: 1}},
	}
	var output bytes.Buffer
	completion := message{JSONRPC: "2.0", ID: json.RawMessage(`1`), Params: mustJSON(map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     map[string]any{"line": 0, "character": len(text)},
	})}
	if err := server.complete(&output, completion); err != nil {
		t.Fatal(err)
	}
	assertQueueLines(t, queue, 0)

	stale := message{JSONRPC: "2.0", ID: json.RawMessage(`2`), Params: mustJSON(map[string]any{
		"command":   "hq.submit",
		"arguments": []any{map[string]any{"uri": uri, "version": 2}},
	})}
	if err := server.executeCommand(&output, stale); err != nil {
		t.Fatal(err)
	}
	assertQueueLines(t, queue, 0)

	valid := message{JSONRPC: "2.0", ID: json.RawMessage(`3`), Params: mustJSON(map[string]any{
		"command":   "hq.submit",
		"arguments": []any{map[string]any{"uri": uri, "version": 1}},
	})}
	if err := server.executeCommand(&output, valid); err != nil {
		t.Fatal(err)
	}
	if err := server.executeCommand(&output, valid); err != nil {
		t.Fatal(err)
	}
	rows := readQueueRows(t, queue)
	if len(rows) != 2 {
		t.Fatalf("rows=%d", len(rows))
	}
	first := rows[0]["instruction"].(map[string]any)
	second := rows[1]["instruction"].(map[string]any)
	if first["id"] == second["id"] || first["id"] == "source-id" || second["id"] == "source-id" {
		t.Fatalf("explicit acceptance ids are not distinct: %v %v", first["id"], second["id"])
	}
	firstPayload, _ := json.Marshal(first["payload"])
	secondPayload, _ := json.Marshal(second["payload"])
	if string(firstPayload) != string(secondPayload) {
		t.Fatalf("semantic payload changed: %s %s", firstPayload, secondPayload)
	}
}

func TestByteOffsetUsesUTF16CodeUnits(t *testing.T) {
	text := "a😀b"
	offset, err := byteOffset(text, 0, 3)
	if err != nil {
		t.Fatal(err)
	}
	if offset != len("a😀") {
		t.Fatalf("offset=%d", offset)
	}
}

func assertQueueLines(t *testing.T, path string, expected int) {
	t.Helper()
	if rows := readQueueRows(t, path); len(rows) != expected {
		t.Fatalf("queue rows=%d expected=%d", len(rows), expected)
	}
}

func readQueueRows(t *testing.T, path string) []map[string]any {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	for _, line := range strings.Split(string(content), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatal(err)
		}
		rows = append(rows, row)
	}
	return rows
}
