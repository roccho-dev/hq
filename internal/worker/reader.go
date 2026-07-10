package worker

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

const maxJSONLLineBytes = 4 << 20

var instructionFields = map[string]struct{}{
	"id": {}, "version": {}, "op": {}, "target": {}, "payload": {}, "created_at": {},
	"reason": {}, "policy": {}, "reply_to": {}, "labels": {},
}

// ReadInstructions reads every non-empty JSONL line independently. Empty lines
// are ignored. Comments are unsupported and therefore reported as malformed
// JSON; hidden queue syntax is never invented by the reader.
func ReadInstructions(path string, r io.Reader) ([]ReadRow, error) {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 64*1024), maxJSONLLineBytes)
	rows := make([]ReadRow, 0)
	line := 0
	for s.Scan() {
		line++
		raw := s.Text()
		if strings.TrimSpace(raw) == "" {
			continue
		}
		row := ReadRow{Source: SourceRef{Path: path, Line: line, Raw: raw}}
		var value any
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			row.ParseError = &Diagnostic{Code: "malformed_json", Message: err.Error()}
			rows = append(rows, row)
			continue
		}
		if _, ok := value.(map[string]any); !ok {
			row.ParseError = &Diagnostic{Code: "invalid_row", Message: "instruction row must be a JSON object"}
			rows = append(rows, row)
			continue
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal([]byte(raw), &fields); err != nil {
			row.ParseError = &Diagnostic{Code: "malformed_json", Message: err.Error()}
			rows = append(rows, row)
			continue
		}
		if err := json.Unmarshal([]byte(raw), &row.Instruction); err != nil {
			row.ParseError = &Diagnostic{Code: "invalid_field_type", Message: err.Error()}
			rows = append(rows, row)
			continue
		}
		for key := range fields {
			if _, ok := instructionFields[key]; !ok {
				row.UnknownFields = append(row.UnknownFields, key)
			}
		}
		sort.Strings(row.UnknownFields)
		rows = append(rows, row)
	}
	if err := s.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return rows, nil
}
