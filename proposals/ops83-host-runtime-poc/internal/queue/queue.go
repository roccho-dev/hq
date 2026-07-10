package queue

import (
	"bufio"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

const (
	HostOpenRequestKind = "hq.hostOpenRequest.v1"
	QueuedKind          = "hq.hostCommandQueued.v1"
	ReceiptKind         = "hq.hostCommandReceipt.v1"
	SubmitResultKind    = "hq.submitResult.v1"
)

type HostOpenRequest struct {
	Kind string `json:"kind"`
	Path string `json:"path"`
}

type Row struct {
	Kind          string `json:"kind"`
	ID            string `json:"id"`
	Status        string `json:"status"`
	Command       string `json:"command"`
	Path          string `json:"path"`
	Profile       string `json:"profile"`
	BufferURI     string `json:"bufferUri"`
	BufferVersion int    `json:"bufferVersion"`
	BufferSHA256  string `json:"bufferSha256"`
	ConfirmedBy   string `json:"confirmedBy"`
	QueuedAt      string `json:"queuedAt"`
}

type Receipt struct {
	Kind       string   `json:"kind"`
	ID         string   `json:"id"`
	QueueID    string   `json:"queueId"`
	Status     string   `json:"status"`
	Executable string   `json:"executable"`
	Args       []string `json:"args"`
	PID        int      `json:"pid,omitempty"`
	Error      string   `json:"error,omitempty"`
	RecordedAt string   `json:"recordedAt"`
}

type SubmitResult struct {
	Kind      string `json:"kind"`
	Status    string `json:"status"`
	QueueKind string `json:"queueKind"`
	QueueID   string `json:"queueId"`
}

func ParseHostOpenRequest(text string) (HostOpenRequest, error) {
	var request HostOpenRequest
	if err := json.Unmarshal([]byte(text), &request); err != nil {
		return request, fmt.Errorf("invalid JSON: %w", err)
	}
	if request.Kind != HostOpenRequestKind {
		return request, fmt.Errorf("kind must be %s", HostOpenRequestKind)
	}
	if request.Path == "" {
		return request, fmt.Errorf("path is required")
	}
	if _, err := os.Stat(request.Path); err != nil {
		return request, fmt.Errorf("path is not accessible: %w", err)
	}
	return request, nil
}

func NewRow(profile, uri string, version int, text string, request HostOpenRequest) (Row, error) {
	digest := sha256.Sum256([]byte(text))
	digestText := hex.EncodeToString(digest[:])
	idBytes := make([]byte, 12)
	if _, err := rand.Read(idBytes); err != nil {
		return Row{}, fmt.Errorf("generate queue id: %w", err)
	}
	return Row{
		Kind:          QueuedKind,
		ID:            "hqcmd_" + hex.EncodeToString(idBytes),
		Status:        "queued",
		Command:       "host.open",
		Path:          request.Path,
		Profile:       profile,
		BufferURI:     uri,
		BufferVersion: version,
		BufferSHA256:  digestText,
		ConfirmedBy:   "vim-lsp",
		QueuedAt:      time.Now().UTC().Format(time.RFC3339Nano),
	}, nil
}

func Append(path string, value any) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n'))
	return err
}

func ReadRows(path string) ([]Row, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var rows []Row
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if len(scanner.Bytes()) == 0 {
			continue
		}
		var row Row
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			return nil, err
		}
		if row.Kind != QueuedKind || row.ID == "" || row.Command != "host.open" || row.Path == "" {
			return nil, fmt.Errorf("invalid %s row", QueuedKind)
		}
		rows = append(rows, row)
	}
	return rows, scanner.Err()
}

func ReadReceiptQueueIDs(path string) (map[string]bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[string]bool{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if len(scanner.Bytes()) == 0 {
			continue
		}
		var receipt Receipt
		if err := json.Unmarshal(scanner.Bytes(), &receipt); err != nil {
			return nil, err
		}
		if receipt.Kind == ReceiptKind && receipt.QueueID != "" {
			out[receipt.QueueID] = true
		}
	}
	return out, scanner.Err()
}
