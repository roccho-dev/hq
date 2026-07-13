package hqlsp

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hq/internal/core"
	"hq/internal/hq"
	"hq/internal/hqprofile"
)

func TestSelectedCommandSubmitBindsWorldCommandAndFreshInstruction(t *testing.T) {
	root := t.TempDir()
	queue := filepath.Join(root, "accepted.jsonl")
	if err := os.WriteFile(queue, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	world, err := hq.LoadSelectedWorldJSONL(strings.NewReader(
		`{"kind":"hq.world.v1","world_id":"world.lsp-proof"}` + "\n" +
			`{"kind":"hq.command.v1","command_id":"proof.run","command_version":"1","name":"proof.run","instruction":{"version":"instruction.v1","op":"run","target":"sh","payload":{"argv":["proof"]}}}`))
	if err != nil {
		t.Fatal(err)
	}
	uri := "file:///proof.hq"
	server := &Server{
		profile: hqprofile.Profile{Name: "local", DeploymentID: "dep-proof", AcceptedPath: queue},
		world: world, documents: map[string]document{uri: {Text: "@proof.run", Version: 1}},
	}
	request := message{JSONRPC: "2.0", ID: json.RawMessage(`1`), Params: mustJSON(map[string]any{
		"command": "hq.submit", "arguments": []any{map[string]any{"uri": uri, "version": 1, "line": 0}},
	})}
	var output bytes.Buffer
	if err := server.executeCommand(&output, request); err != nil {
		t.Fatal(err)
	}
	rows := readQueueRows(t, queue)
	if len(rows) != 1 {
		t.Fatalf("rows=%d", len(rows))
	}
	instruction := rows[0]["instruction"].(map[string]any)
	provenance := rows[0]["provenance"].(map[string]any)
	worldRef, _ := world.SelectedRef()
	if provenance["kind"] != core.CompileProvenanceKind || provenance["input_kind"] != core.CommandInputKind {
		t.Fatalf("provenance=%#v", provenance)
	}
	selected := provenance["world"].(map[string]any)
	if selected["world_id"] != worldRef.WorldID || selected["digest"] != worldRef.Digest {
		t.Fatalf("selected=%#v want=%#v", selected, worldRef)
	}
	command := provenance["command"].(map[string]any)
	if command["command_id"] != "proof.run" || command["command_version"] != "1" || command["digest"] == "" {
		t.Fatalf("command=%#v", command)
	}
	digest, _ := core.CanonicalDigest(instruction)
	if provenance["instruction_digest"] != digest {
		t.Fatalf("instruction digest=%v want=%s", provenance["instruction_digest"], digest)
	}
	if instruction["id"] == "" || instruction["created_at"] == "" {
		t.Fatalf("instruction=%#v", instruction)
	}
}

func TestCanonicalSubmitAlwaysReplacesOldIdentityAndTime(t *testing.T) {
	root := t.TempDir()
	queue := filepath.Join(root, "accepted.jsonl")
	if err := os.WriteFile(queue, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	uri := "file:///canonical.json"
	text := `{"id":"old-id","version":"instruction.v1","op":"run","target":"sh","payload":{"argv":["proof"]},"created_at":"2020-01-01T00:00:00Z"}`
	server := &Server{
		profile: hqprofile.Profile{Name: "local", DeploymentID: "dep-proof", AcceptedPath: queue},
		world: hq.DefaultWorld(), documents: map[string]document{uri: {Text: text, Version: 1}},
	}
	request := message{JSONRPC: "2.0", ID: json.RawMessage(`1`), Params: mustJSON(map[string]any{
		"command": "hq.submit", "arguments": []any{map[string]any{"uri": uri, "version": 1, "line": 0}},
	})}
	var output bytes.Buffer
	if err := server.executeCommand(&output, request); err != nil {
		t.Fatal(err)
	}
	instruction := readQueueRows(t, queue)[0]["instruction"].(map[string]any)
	if instruction["id"] == "old-id" || instruction["created_at"] == "2020-01-01T00:00:00Z" {
		t.Fatalf("old identity/time reused: %#v", instruction)
	}
	if _, ok := readQueueRows(t, queue)[0]["provenance"]; ok {
		t.Fatal("legacy identity-free world emitted strong provenance")
	}
}
