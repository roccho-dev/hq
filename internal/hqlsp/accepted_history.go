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

type acceptedHistoryHeap []core.AcceptedHistoryRecord

func (h acceptedHistoryHeap) Len() int { return len(h) }
func (h acceptedHistoryHeap) Less(i, j int) bool {
	if !h[i].AcceptedAt.Equal(h[j].AcceptedAt) { return h[i].AcceptedAt.Before(h[j].AcceptedAt) }
	if h[i].AcceptedID != h[j].AcceptedID { return h[i].AcceptedID > h[j].AcceptedID }
	return h[i].Input.AcceptedInputDigest > h[j].Input.AcceptedInputDigest
}
func (h acceptedHistoryHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *acceptedHistoryHeap) Push(value any) { *h = append(*h, value.(core.AcceptedHistoryRecord)) }
func (h *acceptedHistoryHeap) Pop() any { old := *h; n := len(old); value := old[n-1]; *h = old[:n-1]; return value }

func (fileAcceptedHistoryReader) Read(path string) ([]core.AcceptedHistoryRecord, core.AcceptedHistoryReport, error) {
	file, err := os.Open(path)
	if err != nil { return nil, core.AcceptedHistoryReport{Fatal: true}, err }
	defer file.Close()
	counts := map[string]int{}
	retained := &acceptedHistoryHeap{}
	heap.Init(retained)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), maxAcceptedHistoryLineBytes)
	for scanner.Scan() {
		raw := scanner.Bytes()
		if len(bytes.TrimSpace(raw)) == 0 { continue }
		record, legacy, rowErr, fatalErr := decodeAcceptedHistoryLine(raw)
		if fatalErr != nil { return nil, core.AcceptedHistoryReport{Fatal: true, Findings: core.SortHistoryFindings(counts)}, fatalErr }
		if legacy { counts["legacy-row"]++; continue }
		if rowErr != "" { counts[rowErr]++; continue }
		heap.Push(retained, record)
		if retained.Len() > core.MaxAcceptedHistoryRows { heap.Pop(retained) }
	}
	if err := scanner.Err(); err != nil { return nil, core.AcceptedHistoryReport{Fatal: true, Findings: core.SortHistoryFindings(counts)}, err }
	rows := make([]core.AcceptedHistoryRecord, retained.Len())
	copy(rows, *retained)
	sort.Slice(rows, func(i, j int) bool {
		if !rows[i].AcceptedAt.Equal(rows[j].AcceptedAt) { return rows[i].AcceptedAt.After(rows[j].AcceptedAt) }
		if rows[i].AcceptedID != rows[j].AcceptedID { return rows[i].AcceptedID < rows[j].AcceptedID }
		return rows[i].Input.AcceptedInputDigest < rows[j].Input.AcceptedInputDigest
	})
	return rows, core.AcceptedHistoryReport{Findings: core.SortHistoryFindings(counts)}, nil
}

func decodeAcceptedHistoryLine(raw []byte) (core.AcceptedHistoryRecord, bool, string, error) {
	var envelope historyEnvelope
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil { return core.AcceptedHistoryRecord{}, false, "", err }
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil { err = errors.New("accepted row contains multiple JSON values") }
		return core.AcceptedHistoryRecord{}, false, "", err
	}
	if envelope.Kind != "accepted.instruction" || envelope.Queue != "instruction.jsonl" || len(bytes.TrimSpace(envelope.Instruction)) == 0 {
		return core.AcceptedHistoryRecord{}, false, "", errors.New("invalid accepted envelope")
	}
	if envelope.Provenance == nil || envelope.Provenance.Command == nil {
		if len(envelope.AcceptedInput) == 0 { return core.AcceptedHistoryRecord{}, true, "", nil }
		return core.AcceptedHistoryRecord{}, false, "", errors.New("accepted input row lacks compile provenance")
	}
	if err := envelope.Provenance.Validate(); err != nil { return core.AcceptedHistoryRecord{}, false, "", err }
	var instruction map[string]any
	instructionDecoder := json.NewDecoder(bytes.NewReader(envelope.Instruction))
	instructionDecoder.UseNumber()
	if err := instructionDecoder.Decode(&instruction); err != nil { return core.AcceptedHistoryRecord{}, false, "", err }
	instructionDigest, err := core.CanonicalDigest(instruction)
	if err != nil || instructionDigest != envelope.Provenance.InstructionDigest { return core.AcceptedHistoryRecord{}, false, "", errors.New("accepted instruction provenance mismatch") }
	if len(envelope.AcceptedInput) == 0 { return core.AcceptedHistoryRecord{}, true, "", nil }
	var input core.AcceptedInput
	inputDecoder := json.NewDecoder(bytes.NewReader(envelope.AcceptedInput))
	inputDecoder.UseNumber()
	inputDecoder.DisallowUnknownFields()
	if err := inputDecoder.Decode(&input); err != nil { return core.AcceptedHistoryRecord{}, false, "invalid-accepted-input", nil }
	var inputExtra any
	if err := inputDecoder.Decode(&inputExtra); !errors.Is(err, io.EOF) { return core.AcceptedHistoryRecord{}, false, "invalid-accepted-input", nil }
	if err := input.Validate(); err != nil { return core.AcceptedHistoryRecord{}, false, "invalid-accepted-input", nil }
	id, _ := instruction["id"].(string)
	createdText, _ := instruction["created_at"].(string)
	created, err := time.Parse(time.RFC3339Nano, createdText)
	if strings.TrimSpace(id) == "" || err != nil { return core.AcceptedHistoryRecord{}, false, "missing-accepted-identity", nil }
	return core.AcceptedHistoryRecord{AcceptedID: id, AcceptedAt: created.UTC(), Provenance: *envelope.Provenance, Input: input}, false, "", nil
}

func mergeHistoryReports(left, right core.AcceptedHistoryReport) core.AcceptedHistoryReport {
	counts := map[string]int{}
	for _, finding := range append(append([]core.AcceptedHistoryFinding(nil), left.Findings...), right.Findings...) { counts[finding.Code] += finding.Count }
	return core.AcceptedHistoryReport{Fatal: left.Fatal || right.Fatal, Findings: core.SortHistoryFindings(counts)}
}

func historyReadErrorReport(err error) core.AcceptedHistoryReport {
	if err == nil { return core.AcceptedHistoryReport{} }
	return core.AcceptedHistoryReport{Fatal: true, Findings: []core.AcceptedHistoryFinding{{Code: fmt.Sprintf("history-read-failed:%T", err), Count: 1}}}
}
