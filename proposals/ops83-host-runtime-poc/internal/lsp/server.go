package lsp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"

	"github.com/roccho-dev/ops/packages/hq/internal/profile"
	"github.com/roccho-dev/ops/packages/hq/internal/queue"
)

type document struct {
	Text    string
	Version int
}

type Server struct {
	profile   *profile.Profile
	documents map[string]document
}

func New(p *profile.Profile) *Server {
	return &Server{profile: p, documents: map[string]document{}}
}

func (s *Server) Serve(r io.Reader, w io.Writer) error {
	reader := bufio.NewReader(r)
	for {
		msg, err := readMessage(reader)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if msg.Method == "exit" {
			return nil
		}
		if err := s.handle(w, msg); err != nil {
			return err
		}
	}
}

func (s *Server) handle(w io.Writer, msg message) error {
	switch msg.Method {
	case "initialize":
		return writeMessage(w, message{JSONRPC: "2.0", ID: msg.ID, Result: map[string]any{
			"capabilities": map[string]any{
				"textDocumentSync":       1,
				"completionProvider":     map[string]any{"triggerCharacters": []string{"{", "\"", ":", ","}},
				"codeActionProvider":     true,
				"executeCommandProvider": map[string]any{"commands": []string{"hq.submit"}},
			},
			"serverInfo": map[string]any{"name": "hq", "version": "0.1.0"},
		}})
	case "initialized":
		return nil
	case "shutdown":
		return writeMessage(w, message{JSONRPC: "2.0", ID: msg.ID, Result: nil})
	case "textDocument/didOpen":
		var p struct {
			TextDocument struct {
				URI     string `json:"uri"`
				Text    string `json:"text"`
				Version int    `json:"version"`
			} `json:"textDocument"`
		}
		if err := json.Unmarshal(msg.Params, &p); err != nil {
			return err
		}
		s.documents[p.TextDocument.URI] = document{Text: p.TextDocument.Text, Version: p.TextDocument.Version}
		return s.publishDiagnostics(w, p.TextDocument.URI)
	case "textDocument/didChange":
		var p struct {
			TextDocument struct {
				URI     string `json:"uri"`
				Version int    `json:"version"`
			} `json:"textDocument"`
			ContentChanges []struct {
				Text string `json:"text"`
			} `json:"contentChanges"`
		}
		if err := json.Unmarshal(msg.Params, &p); err != nil {
			return err
		}
		if len(p.ContentChanges) > 0 {
			s.documents[p.TextDocument.URI] = document{Text: p.ContentChanges[len(p.ContentChanges)-1].Text, Version: p.TextDocument.Version}
		}
		return s.publishDiagnostics(w, p.TextDocument.URI)
	case "textDocument/completion":
		items := make([]map[string]any, 0, len(s.profile.Commands))
		for _, spec := range s.profile.Commands {
			items = append(items, map[string]any{
				"label": spec.Label, "detail": spec.Detail, "kind": 14,
				"insertText": spec.InsertText, "insertTextFormat": 1,
				"data": map[string]any{"commandSpecId": spec.ID, "source": s.profile.CatalogPath},
			})
		}
		return writeMessage(w, message{JSONRPC: "2.0", ID: msg.ID, Result: map[string]any{"isIncomplete": false, "items": items}})
	case "textDocument/codeAction":
		uri, err := uriFromTextDocumentParams(msg.Params)
		if err != nil {
			return writeError(w, msg.ID, -32602, err.Error())
		}
		doc, ok := s.documents[uri]
		if !ok {
			return writeMessage(w, message{JSONRPC: "2.0", ID: msg.ID, Result: []any{}})
		}
		if _, err := queue.ParseHostOpenRequest(doc.Text); err != nil {
			return writeMessage(w, message{JSONRPC: "2.0", ID: msg.ID, Result: []any{}})
		}
		actions := []map[string]any{{
			"title": "HQ Submit host.open", "kind": "quickfix",
			"command": map[string]any{
				"title": "HQ Submit host.open", "command": "hq.submit",
				"arguments": []any{map[string]any{"uri": uri, "version": doc.Version}},
			},
		}}
		return writeMessage(w, message{JSONRPC: "2.0", ID: msg.ID, Result: actions})
	case "workspace/executeCommand":
		return s.executeCommand(w, msg)
	default:
		if len(msg.ID) != 0 {
			return writeError(w, msg.ID, -32601, "method not found")
		}
		return nil
	}
}

func (s *Server) executeCommand(w io.Writer, msg message) error {
	var p struct {
		Command   string `json:"command"`
		Arguments []struct {
			URI     string `json:"uri"`
			Version int    `json:"version"`
		} `json:"arguments"`
	}
	if err := json.Unmarshal(msg.Params, &p); err != nil {
		return writeError(w, msg.ID, -32602, err.Error())
	}
	if p.Command != "hq.submit" || len(p.Arguments) != 1 {
		return writeError(w, msg.ID, -32602, "hq.submit requires one document argument")
	}
	arg := p.Arguments[0]
	doc, ok := s.documents[arg.URI]
	if !ok || doc.Version != arg.Version {
		return writeError(w, msg.ID, -32602, "document version is missing or stale")
	}
	request, err := queue.ParseHostOpenRequest(doc.Text)
	if err != nil {
		return writeError(w, msg.ID, -32602, err.Error())
	}
	row, err := queue.NewRow(s.profile.Name, arg.URI, doc.Version, doc.Text, request)
	if err != nil {
		return writeError(w, msg.ID, -32603, err.Error())
	}
	if err := queue.Append(s.profile.QueuePath, row); err != nil {
		return writeError(w, msg.ID, -32603, err.Error())
	}
	result := queue.SubmitResult{Kind: queue.SubmitResultKind, Status: "queued", QueueKind: row.Kind, QueueID: row.ID}
	return writeMessage(w, message{JSONRPC: "2.0", ID: msg.ID, Result: result})
}

func (s *Server) publishDiagnostics(w io.Writer, uri string) error {
	doc := s.documents[uri]
	diagnostics := []map[string]any{}
	if _, err := queue.ParseHostOpenRequest(doc.Text); err != nil {
		diagnostics = append(diagnostics, map[string]any{
			"range": map[string]any{
				"start": map[string]int{"line": 0, "character": 0},
				"end":   map[string]int{"line": 0, "character": len(doc.Text)},
			},
			"severity": 1, "source": "hq", "code": "invalid-host-open-request", "message": err.Error(),
		})
	}
	return writeMessage(w, message{JSONRPC: "2.0", Method: "textDocument/publishDiagnostics", Result: nil, Params: mustJSON(map[string]any{
		"uri": uri, "version": doc.Version, "diagnostics": diagnostics,
	})})
}

func uriFromTextDocumentParams(raw json.RawMessage) (string, error) {
	var p struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", err
	}
	if p.TextDocument.URI == "" {
		return "", fmt.Errorf("textDocument.uri is required")
	}
	return p.TextDocument.URI, nil
}

func writeError(w io.Writer, id json.RawMessage, code int, text string) error {
	return writeMessage(w, message{JSONRPC: "2.0", ID: id, Error: map[string]any{"code": code, "message": text}})
}

func mustJSON(value any) json.RawMessage {
	b, _ := json.Marshal(value)
	return b
}
