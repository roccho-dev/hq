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

func TestResourceInvocationUsesExistingExplicitSubmitWithoutCommandWrapper(t *testing.T) {
	world, err := hq.LoadSchemaJSONL(strings.NewReader(
		`{"kind":"hq.world.v1","world_id":"world.resource-submit-test"}` + "\n" +
			`{"kind":"hq.local-tool.v1","tool_id":"aws","tool_version":"2.35.11","binding_ref":"local-tool.aws-restricted","binding_contract_version":"1","invocation":{"policy_version":"aws-restricted.v1","max_argv":16,"max_arg_bytes":4096,"limits":{"timeout_ms":60000,"stdout_bytes":4096,"stderr_bytes":4096}}}` + "\n"))
	if err != nil {
		t.Fatal(err)
	}
	queue := filepath.Join(t.TempDir(), "accepted.jsonl")
	if err := os.WriteFile(queue, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	uri := "file:///resource-invocation.json"
	text := `{"id":"source-id","version":"instruction.v1","op":"run","target":"local-tool","payload":{"tool_id":"aws","tool_version":"2.35.11","policy_version":"aws-restricted.v1","argv":["sts","get-caller-identity"]},"created_at":"2026-07-15T07:00:00Z"}`
	server := &Server{
		profile: hqprofile.Profile{Name: "local", DeploymentID: "dep-resource-submit", AcceptedPath: queue},
		world: world, documents: map[string]document{uri: {Text: text, Version: 1}},
	}
	var response bytes.Buffer
	submit := message{JSONRPC: "2.0", ID: json.RawMessage(`1`), Params: mustJSON(map[string]any{
		"command": "hq.submit", "arguments": []any{map[string]any{"uri": uri, "version": 1, "line": 0}},
	})}
	if err := server.executeCommand(&response, submit); err != nil {
		t.Fatal(err)
	}
	rows := readQueueRows(t, queue)
	if len(rows) != 1 {
		t.Fatalf("rows=%d response=%s", len(rows), response.String())
	}
	instruction := rows[0]["instruction"].(map[string]any)
	if instruction["target"] != "local-tool" || instruction["id"] == "source-id" {
		t.Fatalf("instruction=%#v", instruction)
	}
	payload := instruction["payload"].(map[string]any)
	if payload["tool_id"] != "aws" || payload["policy_version"] != "aws-restricted.v1" {
		t.Fatalf("payload=%#v", payload)
	}
	if _, exists := payload["action_id"]; exists {
		t.Fatalf("submit introduced a per-subcommand action: %#v", payload)
	}
	argv := payload["argv"].([]any)
	if len(argv) != 2 || argv[0] != "sts" || argv[1] != "get-caller-identity" {
		t.Fatalf("argv=%#v", argv)
	}
}
