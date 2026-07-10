package lsp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/roccho-dev/ops/packages/hq/internal/profile"
	"github.com/roccho-dev/ops/packages/hq/internal/queue"
)

func TestBufferDidChangeToJSONLCompletionAndSubmitQueue(t *testing.T) {
	root := prepareRoot(t)
	p, err := profile.Load(root, "local")
	if err != nil {
		t.Fatal(err)
	}
	uri := "file:///request.hqjson"
	target := t.TempDir()
	bufferText, _ := json.Marshal(queue.HostOpenRequest{Kind: queue.HostOpenRequestKind, Path: target})

	var input bytes.Buffer
	request := func(id int, method string, params any) {
		idJSON, _ := json.Marshal(id)
		if err := writeMessage(&input, message{JSONRPC: "2.0", ID: idJSON, Method: method, Params: mustJSON(params)}); err != nil {
			t.Fatal(err)
		}
	}
	notify := func(method string, params any) {
		if err := writeMessage(&input, message{JSONRPC: "2.0", Method: method, Params: mustJSON(params)}); err != nil {
			t.Fatal(err)
		}
	}
	request(1, "initialize", map[string]any{})
	notify("initialized", map[string]any{})
	notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 1, "text": "{}"}})
	notify("textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": uri, "version": 2},
		"contentChanges": []any{map[string]any{"text": string(bufferText)}},
	})
	request(2, "textDocument/completion", map[string]any{"textDocument": map[string]any{"uri": uri}, "position": map[string]int{"line": 0, "character": 0}})
	request(3, "textDocument/codeAction", map[string]any{"textDocument": map[string]any{"uri": uri}})
	request(4, "workspace/executeCommand", map[string]any{"command": "hq.submit", "arguments": []any{map[string]any{"uri": uri, "version": 2}}})
	notify("exit", map[string]any{})

	var output bytes.Buffer
	if err := New(p).Serve(&input, &output); err != nil {
		t.Fatal(err)
	}
	responses := responseByID(t, output.Bytes())
	completion := responses[2]
	result := completion.Result.(map[string]any)
	items := result["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["label"] != "explorer.open" {
		t.Fatalf("completion is not catalog-derived: %#v", items)
	}
	submit := responses[4].Result.(map[string]any)
	if submit["kind"] != queue.SubmitResultKind || submit["status"] != "queued" {
		t.Fatalf("unexpected submit result: %#v", submit)
	}
	rows, err := queue.ReadRows(p.QueuePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Path != target || rows[0].BufferVersion != 2 {
		t.Fatalf("queue row did not come from didChange buffer: %#v", rows)
	}
}

func responseByID(t *testing.T, data []byte) map[int]message {
	t.Helper()
	out := map[int]message{}
	r := bufio.NewReader(bytes.NewReader(data))
	for {
		msg, err := readMessage(r)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if len(msg.ID) == 0 {
			continue
		}
		var id int
		if err := json.Unmarshal(msg.ID, &id); err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(msg.Result)
		var decoded any
		_ = json.Unmarshal(b, &decoded)
		msg.Result = decoded
		out[id] = msg
	}
	return out
}

func prepareRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{filepath.Join(root, "profiles", "local"), filepath.Join(root, "queues"), filepath.Join(root, "receipts")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	catalog := `{"kind":"hq.commandSpec.v1","id":"host.open","label":"explorer.open","detail":"open","insertText":"{}","bufferKind":"hq.hostOpenRequest.v1"}` + "\n"
	for path, content := range map[string]string{
		filepath.Join(root, "profiles", "local", "catalog.jsonl"):        catalog,
		filepath.Join(root, "queues", "hq.host-command.queue.jsonl"):     "",
		filepath.Join(root, "receipts", "hq.host-command.receipt.jsonl"): "",
	} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}
