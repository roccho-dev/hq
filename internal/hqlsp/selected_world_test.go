package hqlsp

import (
	"bufio"
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

func TestInitializeReportsStartupStrictSelectionWithoutWritesOrHotReload(t *testing.T) {
	root := t.TempDir()
	worldPath := filepath.Join(root, "world.jsonl")
	acceptedPath := filepath.Join(root, "accepted.jsonl")
	worldBefore := `{"kind":"hq.world.v1","world_id":"world.lsp-before"}` + "\n" +
		`{"key":"reason","type":"string","required":true}` + "\n"
	if err := os.WriteFile(worldPath, []byte(worldBefore), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(acceptedPath, []byte("sentinel\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	profile := hqprofile.Profile{
		Name: "local", DeploymentID: "deployment-lsp", WorldPath: worldPath,
		AcceptedPath: acceptedPath, EventsPath: filepath.Join(root, "events.jsonl"),
	}
	server, err := New(profile)
	if err != nil {
		t.Fatal(err)
	}
	loaded := server.selectedWorld
	worldAfter := strings.Replace(worldBefore, "world.lsp-before", "world.lsp-after", 1)
	if err := os.WriteFile(worldPath, []byte(worldAfter), 0o600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := server.handle(&output, message{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "initialize"}); err != nil {
		t.Fatal(err)
	}
	response, err := readMessage(bufio.NewReader(&output))
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(response.Result)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Capabilities struct {
			Experimental struct {
				HQ struct {
					Kind         string        `json:"kind"`
					Runtime      string        `json:"runtime"`
					Profile      string        `json:"profile"`
					DeploymentID string        `json:"deployment_id"`
					World        core.WorldRef `json:"world"`
				} `json:"hq"`
			} `json:"experimental"`
		} `json:"capabilities"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	hq := result.Capabilities.Experimental.HQ
	if hq.Kind != RuntimeSelectionKind || hq.Runtime != "lsp" || hq.Profile != profile.Name || hq.DeploymentID != profile.DeploymentID || hq.World != loaded || hq.World.WorldID != "world.lsp-before" {
		t.Fatalf("runtime selection=%+v loaded=%+v", hq, loaded)
	}
	if got, err := os.ReadFile(acceptedPath); err != nil || string(got) != "sentinel\n" {
		t.Fatalf("initialize changed accepted evidence: %q err=%v", got, err)
	}
	if _, err := os.Stat(profile.EventsPath); !os.IsNotExist(err) {
		t.Fatalf("initialize wrote event/history evidence: %v", err)
	}
}

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
		world:   world, documents: map[string]document{uri: {Text: "@proof.run", Version: 1}},
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
		world:   hq.DefaultWorld(), documents: map[string]document{uri: {Text: text, Version: 1}},
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
