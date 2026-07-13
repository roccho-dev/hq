// Package hqlsp exposes the canonical hq compiler through LSP. Completion and
// diagnostics are side-effect free; only hq.submit appends one accepted envelope.
package hqlsp

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"hq/internal/core"
	"hq/internal/hq"
	"hq/internal/hqprofile"
	"hq/internal/selectedworld"
)

const (
	SubmitResultKind     = "hq.submitResult.v1"
	DraftConsumptionKind = "hq.draftConsumption.v1"
	RuntimeSelectionKind = "hq.runtimeSelection.v1"
)

type document struct {
	Text    string
	Version int
}

type Server struct {
	profile       hqprofile.Profile
	world         *hq.JsonlWorld
	recall        *core.WorldRecallIndex
	selectedWorld hq.WorldRef
	selected      bool
	documents     map[string]document
}

func New(profile hqprofile.Profile) (*Server, error) {
	selection, err := selectedworld.LoadRuntime(profile.WorldPath)
	if err != nil {
		return nil, fmt.Errorf("load profile world: %w", err)
	}
	recall, err := hq.PrepareWorldRecall(selection.World)
	if err != nil {
		return nil, fmt.Errorf("prepare profile world recall: %w", err)
	}
	return &Server{
		profile: profile, world: selection.World, recall: recall,
		selectedWorld: selection.Ref, selected: selection.Selected,
		documents: map[string]document{},
	}, nil
}

func (s *Server) Serve(r io.Reader, w io.Writer) error {
	reader := bufio.NewReader(r)
	for {
		msg, err := readMessage(reader)
		if errors.Is(err, io.EOF) {
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
		capabilities := map[string]any{
			"textDocumentSync":       1,
			"completionProvider":     map[string]any{"triggerCharacters": []string{"@", "{", "\"", ":", ",", ".", " ", "="}},
			"codeActionProvider":     true,
			"executeCommandProvider": map[string]any{"commands": []string{"hq.submit"}},
		}
		if s.selected {
			capabilities["experimental"] = map[string]any{"hq": map[string]any{
				"kind": RuntimeSelectionKind, "runtime": "lsp",
				"profile": s.profile.Name, "deployment_id": s.profile.DeploymentID,
				"world": s.selectedWorld,
			}}
		}
		return writeMessage(w, message{JSONRPC: "2.0", ID: msg.ID, Result: map[string]any{
			"capabilities": capabilities,
			"serverInfo":   map[string]any{"name": "hq", "version": "1"},
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
		if len(p.ContentChanges) == 0 {
			return nil
		}
		s.documents[p.TextDocument.URI] = document{Text: p.ContentChanges[len(p.ContentChanges)-1].Text, Version: p.TextDocument.Version}
		return s.publishDiagnostics(w, p.TextDocument.URI)
	case "textDocument/completion":
		return s.complete(w, msg)
	case "textDocument/codeAction":
		return s.codeAction(w, msg)
	case "workspace/executeCommand":
		return s.executeCommand(w, msg)
	default:
		if len(msg.ID) != 0 {
			return writeError(w, msg.ID, -32601, "method not found")
		}
		return nil
	}
}

func (s *Server) complete(w io.Writer, msg message) error {
	var p struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
		Position struct {
			Line      int `json:"line"`
			Character int `json:"character"`
		} `json:"position"`
	}
	if err := json.Unmarshal(msg.Params, &p); err != nil {
		return writeError(w, msg.ID, -32602, err.Error())
	}
	doc, ok := s.documents[p.TextDocument.URI]
	if !ok {
		return writeError(w, msg.ID, -32602, "document is not open")
	}
	cursor, err := byteOffset(doc.Text, p.Position.Line, p.Position.Character)
	if err != nil {
		return writeError(w, msg.ID, -32602, err.Error())
	}
	var suggestions []hq.Suggestion
	if s.recall != nil {
		suggestions = hq.CompleteWithWorldRecall(doc.Text, cursor, doc.Version, s.world, s.recall)
	} else {
		suggestions = hq.Complete(doc.Text, cursor, s.world)
	}
	items := make([]map[string]any, 0, len(suggestions))
	isIncomplete := s.recall != nil && hq.IsMutableWorldRecall(doc.Text, cursor, s.world)
	for _, suggestion := range suggestions {
		start, startErr := positionAtByte(doc.Text, suggestion.Edit.Start)
		end, endErr := positionAtByte(doc.Text, suggestion.Edit.End)
		if startErr != nil || endErr != nil {
			return writeError(w, msg.ID, -32603, "completion edit is outside document")
		}
		item := map[string]any{
			"label":            suggestion.Label,
			"detail":           suggestion.Detail,
			"documentation":    suggestion.Documentation,
			"sortText":         suggestion.SortText,
			"filterText":       suggestion.FilterText,
			"kind":             14,
			"insertText":       suggestion.InsertText,
			"insertTextFormat": 1,
			"textEdit": map[string]any{
				"range":   map[string]any{"start": start, "end": end},
				"newText": suggestion.Edit.Text,
			},
		}
		if suggestion.Candidate != nil {
			item["data"] = suggestion.Candidate
		} else {
			data := map[string]any{"compileDraft": suggestion.Draft, "deploymentId": s.profile.DeploymentID}
			if worldRef, selected := s.world.SelectedRef(); selected {
				data["world"] = worldRef
			}
			item["data"] = data
		}
		items = append(items, item)
	}
	return writeMessage(w, message{JSONRPC: "2.0", ID: msg.ID, Result: map[string]any{"isIncomplete": isIncomplete, "items": items}})
}

func (s *Server) codeAction(w io.Writer, msg message) error {
	uri, err := uriFromTextDocument(msg.Params)
	if err != nil {
		return writeError(w, msg.ID, -32602, err.Error())
	}
	doc, ok := s.documents[uri]
	if !ok {
		return writeMessage(w, message{JSONRPC: "2.0", ID: msg.ID, Result: []any{}})
	}
	var request struct {
		Range struct {
			Start struct {
				Line int `json:"line"`
			} `json:"start"`
		} `json:"range"`
	}
	if err := json.Unmarshal(msg.Params, &request); err != nil {
		return writeError(w, msg.ID, -32602, err.Error())
	}
	actions := []map[string]any{{
		"title": "HQ Submit",
		"kind":  "quickfix",
		"command": map[string]any{
			"title":     "HQ Submit",
			"command":   "hq.submit",
			"arguments": []any{map[string]any{"uri": uri, "version": doc.Version, "line": request.Range.Start.Line}},
		},
	}}
	return writeMessage(w, message{JSONRPC: "2.0", ID: msg.ID, Result: actions})
}

func (s *Server) executeCommand(w io.Writer, msg message) error {
	var p struct {
		Command   string `json:"command"`
		Arguments []struct {
			URI     string `json:"uri"`
			Version int    `json:"version"`
			Line    int    `json:"line"`
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
	draft, objectRange, err := s.compileSubmit(doc.Text, arg.Line)
	if err != nil {
		return writeError(w, msg.ID, -32602, err.Error())
	}
	var draftConsumption map[string]any
	if objectRange != nil {
		start, startErr := positionAtByte(doc.Text, objectRange.StartByte)
		end, endErr := positionAtByte(doc.Text, objectRange.EndByte)
		if startErr != nil || endErr != nil {
			return writeError(w, msg.ID, -32603, "accepted object range is outside document")
		}
		draftConsumption = map[string]any{
			"kind": DraftConsumptionKind,
			"textDocument": map[string]any{
				"uri":     arg.URI,
				"version": arg.Version,
			},
			"edits": []any{map[string]any{
				"range":   map[string]any{"start": start, "end": end},
				"newText": "",
			}},
		}
	}
	draft, acceptedID, err := s.finalizeExplicitAcceptance(draft)
	if err != nil {
		return writeError(w, msg.ID, -32603, err.Error())
	}
	if err := appendAndSync(s.profile.AcceptedPath, draft); err != nil {
		return writeError(w, msg.ID, -32603, err.Error())
	}
	result := map[string]any{
		"kind":         SubmitResultKind,
		"status":       "queued",
		"queueKind":    draft.Kind,
		"queueId":      acceptedID,
		"deploymentId": s.profile.DeploymentID,
	}
	if draft.Provenance != nil {
		result["world"] = draft.Provenance.World
	}
	if draftConsumption != nil {
		result["draftConsumption"] = draftConsumption
	}
	return writeMessage(w, message{JSONRPC: "2.0", ID: msg.ID, Result: result})
}

func (s *Server) compileSubmit(text string, line int) (hq.CompileDraft, *hq.CommandObjectRange, error) {
	if len(s.world.Commands) == 0 {
		return hq.CompileLine(text, s.world), nil, nil
	}
	commandLine, err := hq.LineAt(text, line)
	if err != nil {
		return hq.CompileDraft{}, nil, err
	}
	if strings.HasPrefix(strings.TrimSpace(commandLine), "{") {
		if !json.Valid([]byte(strings.TrimSpace(commandLine))) {
			return hq.CompileDraft{}, nil, errors.New("submit line is not one complete JSON object")
		}
		return hq.CompileLine(commandLine, s.world), nil, nil
	}
	draft, objectRange, err := hq.CompileSelectedCommandObjectWithRange(text, line, s.world)
	if err != nil {
		return hq.CompileDraft{}, nil, err
	}
	return draft, &objectRange, nil
}

func (s *Server) finalizeExplicitAcceptance(draft hq.CompileDraft) (hq.CompileDraft, string, error) {
	if draft.Instruction == nil {
		return hq.CompileDraft{}, "", errors.New("compile draft has no canonical instruction object")
	}
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return hq.CompileDraft{}, "", fmt.Errorf("generate accepted instruction identity: %w", err)
	}
	acceptedID := "ins-lsp-" + hex.EncodeToString(random)
	draft.Instruction["id"] = acceptedID
	draft.Instruction["created_at"] = time.Now().UTC().Format(time.RFC3339Nano)
	inputKind := hq.CanonicalJSONInputKind
	if draft.Provenance != nil {
		inputKind = draft.Provenance.InputKind
	}
	if err := hq.BindFinalInstructionProvenance(&draft, s.world, inputKind); err != nil {
		return hq.CompileDraft{}, "", err
	}
	return draft, acceptedID, nil
}

func (s *Server) publishDiagnostics(w io.Writer, uri string) error {
	doc := s.documents[uri]
	diagnostics := []map[string]any{}
	if len(s.world.Commands) > 0 {
		for _, diagnostic := range hq.ValidateCommandDocument(doc.Text, s.world) {
			diagnostics = append(diagnostics, map[string]any{
				"range": map[string]any{
					"start": map[string]int{"line": diagnostic.Line, "character": diagnostic.Start},
					"end":   map[string]int{"line": diagnostic.Line, "character": diagnostic.End},
				},
				"severity": 1, "source": "hq", "code": diagnostic.Code, "message": diagnostic.Message,
			})
		}
		return writeMessage(w, message{JSONRPC: "2.0", Method: "textDocument/publishDiagnostics", Params: mustJSON(map[string]any{"uri": uri, "version": doc.Version, "diagnostics": diagnostics})})
	}
	trimmed := strings.TrimSpace(doc.Text)
	if trimmed != "" && !json.Valid([]byte(trimmed)) {
		diagnostics = append(diagnostics, map[string]any{
			"range": map[string]any{
				"start": map[string]int{"line": 0, "character": 0},
				"end":   map[string]int{"line": 0, "character": utf8.RuneCountInString(doc.Text)},
			},
			"severity": 1,
			"source":   "hq",
			"code":     "invalid-json",
			"message":  "buffer is not one complete JSON object",
		})
	}
	return writeMessage(w, message{JSONRPC: "2.0", Method: "textDocument/publishDiagnostics", Params: mustJSON(map[string]any{"uri": uri, "version": doc.Version, "diagnostics": diagnostics})})
}

func positionAtByte(text string, offset int) (map[string]int, error) {
	if offset < 0 || offset > len(text) || !utf8.ValidString(text[:offset]) {
		return nil, errors.New("byte offset is outside document")
	}
	prefix := text[:offset]
	line := strings.Count(prefix, "\n")
	lineStart := strings.LastIndex(prefix, "\n") + 1
	character := len(utf16.Encode([]rune(prefix[lineStart:])))
	return map[string]int{"line": line, "character": character}, nil
}

func appendAndSync(path string, draft hq.CompileDraft) error {
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("open accepted queue: %w", err)
	}
	writer := hq.QueueWriter{W: file}
	if err := writer.Append(draft); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func uriFromTextDocument(raw json.RawMessage) (string, error) {
	var p struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", err
	}
	if p.TextDocument.URI == "" {
		return "", errors.New("textDocument.uri is required")
	}
	return p.TextDocument.URI, nil
}

func byteOffset(text string, line, character int) (int, error) {
	if line < 0 || character < 0 {
		return 0, errors.New("position cannot be negative")
	}
	start := 0
	for current := 0; current < line; current++ {
		index := strings.IndexByte(text[start:], '\n')
		if index < 0 {
			return 0, errors.New("position line is outside document")
		}
		start += index + 1
	}
	end := strings.IndexByte(text[start:], '\n')
	if end < 0 {
		end = len(text) - start
	}
	lineText := text[start : start+end]
	units := 0
	byteIndex := 0
	for byteIndex < len(lineText) {
		r, size := utf8.DecodeRuneInString(lineText[byteIndex:])
		width := len(utf16.Encode([]rune{r}))
		if units+width > character {
			break
		}
		units += width
		byteIndex += size
		if units == character {
			return start + byteIndex, nil
		}
	}
	if units == character {
		return start + byteIndex, nil
	}
	return 0, errors.New("position character is outside document")
}
