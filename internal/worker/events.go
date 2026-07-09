package worker

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

type EventLog struct {
	path string
	mu   sync.Mutex
}

func NewEventLog(path string) *EventLog { return &EventLog{path: path} }

func (l *EventLog) Append(entry LogEntry) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	var value any
	switch {
	case entry.Decision != nil && entry.Result == nil:
		if err := entry.Decision.Validate(); err != nil {
			return err
		}
		value = entry.Decision
	case entry.Result != nil && entry.Decision == nil:
		if err := entry.Result.Validate(); err != nil {
			return err
		}
		value = entry.Result
	default:
		return fmt.Errorf("log entry must contain exactly one row")
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	n, err := f.Write(b)
	if err != nil {
		return err
	}
	if n != len(b) {
		return io.ErrShortWrite
	}
	return f.Sync()
}

func LoadEventLog(r io.Reader) (LogData, error) {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 64*1024), maxJSONLLineBytes)
	var data LogData
	seenDecisionIDs := map[string]struct{}{}
	seenEventIDs := map[string]struct{}{}
	line := 0
	for s.Scan() {
		line++
		if len(s.Bytes()) == 0 {
			continue
		}
		var header struct {
			Version string `json:"version"`
		}
		if err := json.Unmarshal(s.Bytes(), &header); err != nil {
			return LogData{}, fmt.Errorf("event line %d: %w", line, err)
		}
		switch header.Version {
		case DecisionVersionV1:
			var row DecisionRow
			if err := decodeStrict(s.Bytes(), &row); err != nil {
				return LogData{}, fmt.Errorf("decision line %d: %w", line, err)
			}
			if err := row.Validate(); err != nil {
				return LogData{}, fmt.Errorf("decision line %d: %w", line, err)
			}
			if _, exists := seenDecisionIDs[row.DecisionID]; exists {
				return LogData{}, fmt.Errorf("decision line %d: duplicate decision_id %q", line, row.DecisionID)
			}
			seenDecisionIDs[row.DecisionID] = struct{}{}
			data.Decisions = append(data.Decisions, row)
		case ResultVersionV1:
			var row ResultRow
			if err := decodeStrict(s.Bytes(), &row); err != nil {
				return LogData{}, fmt.Errorf("result line %d: %w", line, err)
			}
			if err := row.Validate(); err != nil {
				return LogData{}, fmt.Errorf("result line %d: %w", line, err)
			}
			if _, exists := seenEventIDs[row.EventID]; exists {
				return LogData{}, fmt.Errorf("result line %d: duplicate event_id %q", line, row.EventID)
			}
			seenEventIDs[row.EventID] = struct{}{}
			data.Results = append(data.Results, row)
		default:
			return LogData{}, fmt.Errorf("event line %d: unsupported version %q", line, header.Version)
		}
	}
	if err := s.Err(); err != nil {
		return LogData{}, err
	}
	return data, nil
}

func LoadEventFile(path string) (LogData, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return LogData{}, nil
	}
	if err != nil {
		return LogData{}, err
	}
	defer f.Close()
	return LoadEventLog(f)
}
