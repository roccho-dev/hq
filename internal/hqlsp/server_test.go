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

const selectedRecallIdentityFixture = `{"kind":"hq.world.v1","world_id":"world.hq-recall-test"}`
const selectedRecallCommandFixture = `{"kind":"hq.command.v1","command_id":"command.herdr.read","command_version":"v1","name":"herdr.read","aliases":["agent.read"],"keywords":["reviewer"],"description":"read agent output","instruction":{"version":"instruction.v1","op":"run","target":"herdr","payload":{"action":"read"}},"fields":[{"name":"agent","type":"string","required":true,"examples":["reviewer"],"bind":"payload.agent"},{"name":"source","type":"enum","required":true,"enum":["recent-unwrapped","screen"],"bind":"payload.source"},{"name":"lines","type":"integer","required":true,"default":100,"materialized_values":[50,200],"bind":"payload.lines"}],"presets":[{"id":"reviewer-screen","label":"Reviewer screen","values":{"agent":"reviewer","source":"screen","lines":100}}]}`
const selectedRecallWorldFixture = selectedRecallIdentityFixture + "\n" + selectedRecallCommandFixture

type completionResponse struct {
	Result struct {
		IsIncomplete bool                 `json:"isIncomplete"`
		Items        []completionWireItem `json:"items"`
	} `json:"result"`
}

type completionWireItem struct {
	Label         string `json:"label"`
	Detail        string `json:"detail"`
	Documentation string `json:"documentation"`
	SortText      string `json:"sortText"`
	FilterText    string `json:"filterText"`
	TextEdit      struct {
		Range struct {
			Start struct {
				Line      int `json:"line"`
				Character int `json:"character"`
			} `json:"start"`
			End struct {
				Line      int `json:"line"`
				Character int `json:"character"`
			} `json:"end"`
		} `json:"range"`
		NewText string `json:"newText"`
	} `json:"textEdit"`
	Data    json.RawMessage `json:"data"`
	Command json.RawMessage `json:"command"`
}

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

func TestCommandNotebookCompletionDiagnosticsAndCursorLineSubmit(t *testing.T) {
	root := t.TempDir()
	queue := filepath.Join(root, "accepted.jsonl")
	if err := os.WriteFile(queue, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	world, err := hq.LoadSchemaJSONL(strings.NewReader(
		`{"kind":"hq.command.v1","name":"host.open","description":"open host path","instruction":{"version":"instruction.v1","op":"run","target":"host","payload":{"capability":"host.open"}},"fields":[{"name":"path","type":"path","required":true,"bind":"payload.path"}]}` + "\n" +
			`{"kind":"hq.command.v1","name":"herdr.read","description":"read agent","instruction":{"version":"instruction.v1","op":"run","target":"herdr","payload":{"action":"read"}},"fields":[{"name":"agent","type":"string","required":true,"bind":"payload.agent"},{"name":"source","type":"enum","required":true,"enum":["recent-unwrapped","screen"],"bind":"payload.source"},{"name":"lines","type":"integer","required":true,"bind":"payload.lines"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	uri := "file:///commands.hq"
	text := "@host.open\n\n@herdr.read\nagent=reviewer\nsource=screen\nlines=100"
	server := &Server{profile: hqprofile.Profile{Name: "local", DeploymentID: "dep-commands", AcceptedPath: queue}, world: world, documents: map[string]document{uri: {Text: text, Version: 7}}}

	var diagnostics bytes.Buffer
	if err := server.publishDiagnostics(&diagnostics, uri); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diagnostics.String(), `required field \"path\" is missing`) {
		t.Fatalf("diagnostics=%s", diagnostics.String())
	}

	var completion bytes.Buffer
	request := message{JSONRPC: "2.0", ID: json.RawMessage(`1`), Params: mustJSON(map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     map[string]any{"line": 4, "character": len("source=s")},
	})}
	server.documents[uri] = document{Text: "@host.open\n\n@herdr.read\nagent=reviewer\nsource=s\nlines=100", Version: 8}
	if err := server.complete(&completion, request); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(completion.String(), `"label":"screen"`) || !strings.Contains(completion.String(), `"textEdit"`) {
		t.Fatalf("completion=%s", completion.String())
	}
	assertQueueLines(t, queue, 0)

	server.documents[uri] = document{Text: text, Version: 7}
	var submitted bytes.Buffer
	submit := message{JSONRPC: "2.0", ID: json.RawMessage(`2`), Params: mustJSON(map[string]any{
		"command": "hq.submit", "arguments": []any{map[string]any{"uri": uri, "version": 7, "line": 4}},
	})}
	if err := server.executeCommand(&submitted, submit); err != nil {
		t.Fatal(err)
	}
	rows := readQueueRows(t, queue)
	if len(rows) != 1 {
		t.Fatalf("queue rows=%d", len(rows))
	}
	instruction := rows[0]["instruction"].(map[string]any)
	if instruction["target"] != "herdr" || instruction["id"] == "" || instruction["created_at"] == "" {
		t.Fatalf("instruction=%#v", instruction)
	}
	payload := instruction["payload"].(map[string]any)
	if payload["action"] != "read" || payload["agent"] != "reviewer" || payload["lines"] != float64(100) {
		t.Fatalf("payload=%#v", payload)
	}
}

func TestSelectedWorldRecallCompletionTransportsCandidateAndAppendsNothing(t *testing.T) {
	root := t.TempDir()
	queue := filepath.Join(root, "accepted.jsonl")
	if err := os.WriteFile(queue, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	worldPath := filepath.Join(root, "world.jsonl")
	if err := os.WriteFile(worldPath, []byte(selectedRecallWorldFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	server, err := New(hqprofile.Profile{Name: "local", DeploymentID: "dep-recall", WorldPath: worldPath, AcceptedPath: queue})
	if err != nil {
		t.Fatal(err)
	}
	if server.recall == nil {
		t.Fatal("selected world did not prepare a recall index")
	}
	uri := "file:///recall.hq"
	query := "@ reviewer screen"
	server.documents[uri] = document{Text: query, Version: 17}
	before, err := os.ReadFile(queue)
	if err != nil {
		t.Fatal(err)
	}

	var completion bytes.Buffer
	request := message{JSONRPC: "2.0", ID: json.RawMessage(`1`), Params: mustJSON(map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     map[string]any{"line": 0, "character": len(query)},
	})}
	if err := server.complete(&completion, request); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(queue)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatalf("completion changed accepted queue: before=%q after=%q", before, after)
	}
	wire := decodeCompletionResponse(t, completion.String())
	if !wire.Result.IsIncomplete {
		t.Fatalf("selected mutable query was complete: %#v", wire.Result)
	}
	if len(wire.Result.Items) != 2 {
		t.Fatalf("items=%#v", wire.Result.Items)
	}
	const wantIndexID = "sha256:287cea6457dfeba9d6896752ca6c6d56248faeaaf9cbbd40cd48855c436e957f"
	wantCandidateIDs := []string{
		"sha256:e4af66171ece9d26ca3d68c44ed9311bfaa702932aacff682d1809b27375022b",
		"sha256:30043a7d6282aa5d9bcaa9b8ae0108a1a398b77be08e2ec8bf137aceb222c032",
	}
	for index, item := range wire.Result.Items {
		var candidate hq.Candidate
		if err := json.Unmarshal(item.Data, &candidate); err != nil {
			t.Fatalf("item %d candidate: %v", index, err)
		}
		if candidate.Kind != hq.CandidateKind || candidate.CandidateID != wantCandidateIDs[index] || candidate.IndexID != wantIndexID {
			t.Fatalf("item %d candidate identity=%#v", index, candidate)
		}
		if candidate.DocumentVersion != 17 {
			t.Fatalf("item %d document version=%d", index, candidate.DocumentVersion)
		}
		if item.Command != nil {
			t.Fatalf("item %d acquired a completion command: %s", index, item.Command)
		}
		if item.Detail != candidate.Detail || item.Documentation != candidate.Documentation || item.SortText == "" || item.FilterText == "" {
			t.Fatalf("item %d did not map candidate presentation: %#v candidate=%#v", index, item, candidate)
		}
		if item.TextEdit.Range.Start.Line != 0 || item.TextEdit.Range.Start.Character != 0 || item.TextEdit.Range.End.Line != 0 || item.TextEdit.Range.End.Character != len(query) {
			t.Fatalf("item %d textEdit range=%#v", index, item.TextEdit.Range)
		}
		if item.TextEdit.NewText != candidate.Edit.NewText || candidate.Edit.StartByte != 0 || candidate.Edit.EndByte != len(query) {
			t.Fatalf("item %d textEdit=%#v candidate edit=%#v", index, item.TextEdit, candidate.Edit)
		}
		var data map[string]json.RawMessage
		if err := json.Unmarshal(item.Data, &data); err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"compileDraft", "deployment", "deploymentId", "effect", "queue", "provider"} {
			if _, found := data[key]; found {
				t.Fatalf("item %d candidate data contains authority key %q: %s", index, key, item.Data)
			}
		}
		if _, found := data["candidate_id"]; !found {
			t.Fatalf("item %d data is not the candidate object itself: %s", index, item.Data)
		}
	}
	if len(wire.Result.Items) != 2 || wire.Result.Items[0].FilterText != "screen herdr.read" || wire.Result.Items[1].FilterText != "screen Reviewer screen" {
		t.Fatalf("wire filters=%#v", wire.Result.Items)
	}
	if !strings.HasPrefix(wire.Result.Items[0].SortText, "rank-") || !strings.HasSuffix(wire.Result.Items[0].SortText, wantCandidateIDs[0]) || wire.Result.Items[0].SortText >= wire.Result.Items[1].SortText {
		t.Fatalf("wire sort order=%#v", wire.Result.Items)
	}

	server.documents[uri] = document{Text: query, Version: 18}
	var nextCompletion bytes.Buffer
	if err := server.complete(&nextCompletion, request); err != nil {
		t.Fatal(err)
	}
	next := decodeCompletionResponse(t, nextCompletion.String())
	for index, item := range next.Result.Items {
		var candidate hq.Candidate
		if err := json.Unmarshal(item.Data, &candidate); err != nil {
			t.Fatal(err)
		}
		if candidate.CandidateID != wantCandidateIDs[index] || candidate.IndexID != wantIndexID || candidate.DocumentVersion != 18 {
			t.Fatalf("request-local candidate %d=%#v", index, candidate)
		}
	}

	server.documents[uri] = document{Text: "@ unmatched-token", Version: 4}
	var emptyCompletion bytes.Buffer
	emptyRequest := message{JSONRPC: "2.0", ID: json.RawMessage(`4`), Params: mustJSON(map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     map[string]any{"line": 0, "character": len("@ unmatched-token")},
	})}
	if err := server.complete(&emptyCompletion, emptyRequest); err != nil {
		t.Fatal(err)
	}
	if empty := decodeCompletionResponse(t, emptyCompletion.String()); !empty.Result.IsIncomplete || len(empty.Result.Items) != 0 {
		t.Fatalf("mutable empty query response=%s", emptyCompletion.String())
	}
	afterEmpty, err := os.ReadFile(queue)
	if err != nil || !bytes.Equal(afterEmpty, before) {
		t.Fatalf("empty completion changed accepted queue: err=%v before=%q after=%q", err, before, afterEmpty)
	}
}

func TestLegacyIdentityFreeWorldWithDigestUsesOrdinaryCompletionData(t *testing.T) {
	root := t.TempDir()
	queue := filepath.Join(root, "accepted.jsonl")
	worldPath := filepath.Join(root, "world.jsonl")
	legacyCommand := strings.Replace(selectedRecallCommandFixture, `"command_id":"command.herdr.read","command_version":"v1",`, "", 1)
	if err := os.WriteFile(queue, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(worldPath, []byte(legacyCommand), 0o600); err != nil {
		t.Fatal(err)
	}
	server, err := New(hqprofile.Profile{Name: "local", DeploymentID: "dep-legacy", WorldPath: worldPath, AcceptedPath: queue})
	if err != nil {
		t.Fatal(err)
	}
	if server.world.Identity != nil || server.world.Digest == "" || server.recall != nil {
		t.Fatalf("legacy world construction=%#v recall=%#v", server.world, server.recall)
	}
	uri := "file:///legacy.hq"
	server.documents[uri] = document{Text: "@her", Version: 9}
	var output bytes.Buffer
	request := message{JSONRPC: "2.0", ID: json.RawMessage(`1`), Params: mustJSON(map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     map[string]any{"line": 0, "character": len("@her")},
	})}
	if err := server.complete(&output, request); err != nil {
		t.Fatal(err)
	}
	wire := decodeCompletionResponse(t, output.String())
	if wire.Result.IsIncomplete || len(wire.Result.Items) != 1 || wire.Result.Items[0].Label != "herdr.read" {
		t.Fatalf("legacy completion=%#v", wire.Result)
	}
	var data map[string]json.RawMessage
	if err := json.Unmarshal(wire.Result.Items[0].Data, &data); err != nil {
		t.Fatal(err)
	}
	if len(data) != 2 || data["compileDraft"] == nil || string(data["deploymentId"]) != `"dep-legacy"` || data["world"] != nil {
		t.Fatalf("ordinary completion data changed: %s", wire.Result.Items[0].Data)
	}
	var draft hq.CompileDraft
	if err := json.Unmarshal(data["compileDraft"], &draft); err != nil || draft.Kind != "candidate.command" || draft.Queue != "instruction.jsonl" {
		t.Fatalf("ordinary compile draft=%#v err=%v", draft, err)
	}
	assertQueueLines(t, queue, 0)
}

func TestNewRejectsMalformedSelectedWorlds(t *testing.T) {
	duplicateCommand := strings.Replace(selectedRecallCommandFixture, `"name":"herdr.read"`, `"name":"herdr.tail"`, 1)
	tests := map[string]string{
		"missing command identity": selectedRecallIdentityFixture + "\n" + strings.Replace(selectedRecallCommandFixture, `"command_id":"command.herdr.read",`, "", 1),
		"duplicate command id":     selectedRecallWorldFixture + "\n" + duplicateCommand,
	}
	for name, contents := range tests {
		t.Run(name, func(t *testing.T) {
			worldPath := filepath.Join(t.TempDir(), "world.jsonl")
			if err := os.WriteFile(worldPath, []byte(contents), 0o600); err != nil {
				t.Fatal(err)
			}
			server, err := New(hqprofile.Profile{WorldPath: worldPath})
			if err == nil || server != nil || !strings.Contains(err.Error(), "load profile world") {
				t.Fatalf("New() server=%#v err=%v", server, err)
			}
		})
	}
}

func decodeCompletionResponse(t *testing.T, framed string) completionResponse {
	t.Helper()
	parts := strings.SplitN(framed, "\r\n\r\n", 2)
	if len(parts) != 2 {
		t.Fatalf("unframed completion=%s", framed)
	}
	var wire completionResponse
	if err := json.Unmarshal([]byte(parts[1]), &wire); err != nil {
		t.Fatal(err)
	}
	return wire
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
