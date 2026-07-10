// Package workeraccept adapts the hq compiler acceptance envelope into the
// canonical worker instruction reader without adding target meaning or defaults.
package workeraccept

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"hq/internal/worker"
)

const (
	AcceptedKind  = "accepted.instruction"
	AcceptedQueue = "instruction.jsonl"
	maxLineBytes  = 4 << 20
)

type envelope struct {
	Kind        string          `json:"kind"`
	Queue       string          `json:"queue"`
	Key         string          `json:"key,omitempty"`
	Value       json.RawMessage `json:"value,omitempty"`
	Instruction json.RawMessage `json:"instruction"`
	Reason      string          `json:"reason,omitempty"`
}

// Read converts every non-empty accepted.instruction JSONL row into one
// worker.ReadRow. Per-line envelope failures are represented as ParseError so
// the normal worker path persists canonical validation.v1 evidence and
// continues with later rows.
func Read(path string, r io.Reader) ([]worker.ReadRow, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), maxLineBytes)
	var rows []worker.ReadRow
	line := 0
	for scanner.Scan() {
		line++
		raw := scanner.Text()
		if strings.TrimSpace(raw) == "" {
			continue
		}
		source := worker.SourceRef{Path: path, Line: line, Raw: raw}
		instruction, diagnostic := decodeLine([]byte(raw))
		if diagnostic != nil {
			rows = append(rows, worker.ReadRow{Source: source, ParseError: diagnostic})
			continue
		}
		parsed, err := worker.ReadInstructions(path, bytes.NewReader(instruction))
		if err != nil {
			return nil, err
		}
		if len(parsed) != 1 {
			rows = append(rows, worker.ReadRow{Source: source, ParseError: &worker.Diagnostic{
				Code: "invalid_accepted_envelope", Message: "accepted instruction must contain exactly one JSON object",
			}})
			continue
		}
		parsed[0].Source = source
		rows = append(rows, parsed[0])
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return rows, nil
}

func decodeLine(raw []byte) (json.RawMessage, *worker.Diagnostic) {
	var candidate envelope
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&candidate); err != nil {
		return nil, &worker.Diagnostic{Code: "malformed_json", Message: err.Error()}
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			err = errors.New("multiple JSON values in accepted envelope")
		}
		return nil, &worker.Diagnostic{Code: "malformed_json", Message: err.Error()}
	}
	if candidate.Kind != AcceptedKind {
		return nil, &worker.Diagnostic{Code: "invalid_accepted_envelope", Field: "kind", Message: "kind must be accepted.instruction"}
	}
	if candidate.Queue != AcceptedQueue {
		return nil, &worker.Diagnostic{Code: "invalid_accepted_envelope", Field: "queue", Message: "queue must be instruction.jsonl"}
	}
	trimmed := bytes.TrimSpace(candidate.Instruction)
	if len(trimmed) == 0 || trimmed[0] != '{' || trimmed[len(trimmed)-1] != '}' {
		return nil, &worker.Diagnostic{Code: "invalid_accepted_envelope", Field: "instruction", Message: "instruction must be one JSON object"}
	}
	return append(json.RawMessage(nil), trimmed...), nil
}
