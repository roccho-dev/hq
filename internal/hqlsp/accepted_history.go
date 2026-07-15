package hqlsp

import (
	"bufio"
	"bytes"
	"container/heap"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"hq/internal/core"
)

const maxAcceptedHistoryLineBytes = 4 << 20

type acceptedHistoryReader interface {
	Read(path string) ([]core.AcceptedHistoryRecord, core.AcceptedHistoryReport, error)
}

type fileAcceptedHistoryReader struct{}

type historyEnvelope struct {
	Kind          string                  `json:"kind"`
	Queue         string                  `json:"queue"`
	Key           string                  `json:"key,omitempty"`
	Value         json.RawMessage         `json:"value,omitempty"`
	Instruction   json.RawMessage         `json:"instruction"`
	Reason        string                  `json:"reason,omitempty"`
	Provenance    *core.CompileProvenance `json:"provenance,omitempty"`
	AcceptedInput json.RawMessage         `json:"accepted_input,omitempty"`
}

type acceptedHistoryScannedRow struct {
	AcceptedID string
	AcceptedAt time.Time
	Identity   string
	Record     core.AcceptedHistoryRecord
	HasRecord  bool
	Legacy     bool
	Finding    string
}

type acceptedHistoryHeap []acceptedHistoryScannedRow

func (h acceptedHistoryHeap) Len() int { return len(h) }
func (h acceptedHistoryHeap) Less(i, j int) bool {
	if !h[i].AcceptedAt.Equal(h[j].AcceptedAt) {
		return h[i].AcceptedAt.Before(h[j].AcceptedAt)
	}
	if h[i].AcceptedID != h[j].AcceptedID {
		return h[i].AcceptedID > h[j].AcceptedID
	}
	return h[i].Identity > h[j].Identity
}
func (h acceptedHistoryHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *acceptedHistoryHeap) Push(value any) { *h = append(*h, value.(acceptedHistoryScannedRow)) }
func (h *acceptedHistoryHeap) Pop() any {
	old := *h
	n := len(old)
	value := old[n-1]
	*h = old[:n-1]
	return value
}

func (fileAcceptedHistoryReader) Read(path string) ([]core.AcceptedHistoryRecord, core.AcceptedHistoryReport, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, core.AcceptedHistoryReport{Fatal: true}, err
	}
	defer file.Close()
	counts := map[string]int{}
	retained := &acceptedHistoryHeap{}
	heap.Init(retained)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), maxAcceptedHistoryLineBytes)
	for scanner.Scan() {
		raw := scanner.Bytes()
		if len(bytes.TrimSpace(raw)) == 0 {
			continue
		}
		row, fatalErr := decodeAcceptedHistoryLine(raw)
		if fatalErr != nil {
			return nil, core.AcceptedHistoryReport{Fatal: true, Findings: core.SortHistoryFindings(counts)}, fatalErr
		}
		if row.AcceptedID == "" || row.AcceptedAt.IsZero() {
			if row.Finding != "" {
				counts[row.Finding]++
			}
			continue
		}
		heap.Push(retained, row)
		if retained.Len() > core.MaxAcceptedHistoryRows {
			heap.Pop(retained)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, core.AcceptedHistoryReport{Fatal: true, Findings: core.SortHistoryFindings(counts)}, err
	}
	rows := make([]acceptedHistoryScannedRow, retained.Len())
	copy(rows, *retained)
	sort.Slice(rows, func(i, j int) bool {
		if !rows[i].AcceptedAt.Equal(rows[j].AcceptedAt) {
			return rows[i].AcceptedAt.After(rows[j].AcceptedAt)
		}
		if rows[i].AcceptedID != rows[j].AcceptedID {
			return rows[i].AcceptedID < rows[j].AcceptedID
		}
		return rows[i].Identity < rows[j].Identity
	})
	seenID := map[string]string{}
	records := make([]core.AcceptedHistoryRecord, 0, len(rows))
	for _, row := range rows {
		if prior, duplicate := seenID[row.AcceptedID]; duplicate {
			if prior != row.Identity {
				counts["conflicting-accepted-id"]++
				return nil, core.AcceptedHistoryReport{Fatal: true, Findings: core.SortHistoryFindings(counts)}, errors.New("conflicting accepted instruction identity")
			}
			counts["duplicate-accepted-id"]++
			continue
		}
		seenID[row.AcceptedID] = row.Identity
		switch {
		case row.Legacy:
			counts["legacy-row"]++
		case row.Finding != "":
			counts[row.Finding]++
		case row.HasRecord:
			records = append(records, row.Record)
		}
	}
	return records, core.AcceptedHistoryReport{Findings: core.SortHistoryFindings(counts)}, nil
}

func decodeAcceptedHistoryLine(raw []byte) (acceptedHistoryScannedRow, error) {
	var envelope historyEnvelope
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return acceptedHistoryScannedRow{}, err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("accepted row contains multiple JSON values")
		}
		return acceptedHistoryScannedRow{}, err
	}
	if envelope.Kind != "accepted.instruction" || envelope.Queue != "instruction.jsonl" || len(bytes.TrimSpace(envelope.Instruction)) == 0 {
		return acceptedHistoryScannedRow{}, errors.New("invalid accepted envelope")
	}
	var instruction map[string]any
	instructionDecoder := json.NewDecoder(bytes.NewReader(envelope.Instruction))
	instructionDecoder.UseNumber()
	if err := instructionDecoder.Decode(&instruction); err != nil {
		return acceptedHistoryScannedRow{}, err
	}
	var instructionExtra any
	if err := instructionDecoder.Decode(&instructionExtra); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("accepted instruction contains multiple JSON values")
		}
		return acceptedHistoryScannedRow{}, err
	}
	instructionDigest, err := core.CanonicalDigest(instruction)
	if err != nil {
		return acceptedHistoryScannedRow{}, err
	}
	if envelope.Provenance != nil {
		if err := envelope.Provenance.Validate(); err != nil {
			return acceptedHistoryScannedRow{}, err
		}
		if instructionDigest != envelope.Provenance.InstructionDigest {
			return acceptedHistoryScannedRow{}, errors.New("accepted instruction provenance mismatch")
		}
	}
	if len(envelope.AcceptedInput) != 0 && (envelope.Provenance == nil || envelope.Provenance.Command == nil) {
		return acceptedHistoryScannedRow{}, errors.New("accepted input row lacks command compile provenance")
	}
	id, _ := instruction["id"].(string)
	createdText, _ := instruction["created_at"].(string)
	created, timeErr := time.Parse(time.RFC3339Nano, createdText)
	rawInputDigest, _ := core.CanonicalDigest(string(bytes.TrimSpace(envelope.AcceptedInput)))
	row := acceptedHistoryScannedRow{AcceptedID: id, AcceptedAt: created.UTC(), Identity: instructionDigest + "\x00" + rawInputDigest}
	if strings.TrimSpace(id) == "" || timeErr != nil {
		row.AcceptedAt = time.Time{}
		row.Finding = "missing-accepted-identity"
		return row, nil
	}
	if len(envelope.AcceptedInput) == 0 {
		row.Legacy = true
		row.Identity = instructionDigest + "\x00"
		return row, nil
	}
	var input core.AcceptedInput
	inputDecoder := json.NewDecoder(bytes.NewReader(envelope.AcceptedInput))
	inputDecoder.UseNumber()
	inputDecoder.DisallowUnknownFields()
	if err := inputDecoder.Decode(&input); err != nil {
		row.Finding = "invalid-accepted-input"
		return row, nil
	}
	var inputExtra any
	if err := inputDecoder.Decode(&inputExtra); !errors.Is(err, io.EOF) {
		row.Finding = "invalid-accepted-input"
		return row, nil
	}
	if err := input.Validate(); err != nil {
		row.Finding = "invalid-accepted-input"
		return row, nil
	}
	row.Identity = instructionDigest + "\x00" + input.AcceptedInputDigest
	row.Record = core.AcceptedHistoryRecord{AcceptedID: id, AcceptedAt: created.UTC(), Provenance: *envelope.Provenance, Input: input}
	row.HasRecord = true
	return row, nil
}

func mergeHistoryReports(left, right core.AcceptedHistoryReport) core.AcceptedHistoryReport {
	counts := map[string]int{}
	for _, finding := range append(append([]core.AcceptedHistoryFinding(nil), left.Findings...), right.Findings...) {
		counts[finding.Code] += finding.Count
	}
	return core.AcceptedHistoryReport{Fatal: left.Fatal || right.Fatal, Findings: core.SortHistoryFindings(counts)}
}

func historyReadErrorReport(err error) core.AcceptedHistoryReport {
	if err == nil {
		return core.AcceptedHistoryReport{}
	}
	return core.AcceptedHistoryReport{Fatal: true, Findings: []core.AcceptedHistoryFinding{{Code: fmt.Sprintf("history-read-failed:%T", err), Count: 1}}}
}
