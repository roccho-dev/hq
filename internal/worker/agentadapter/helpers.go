package agentadapter

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"hq/internal/worker/adapter"
)

func decodeStrict(raw json.RawMessage, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("payload contains multiple JSON values")
		}
		return err
	}
	return nil
}

func validateAdapterRequest(request adapter.Request, target string) error {
	if err := request.Validate(); err != nil {
		return blocked("invalid_request", err.Error())
	}
	if request.Target != target {
		return blocked("target_mismatch", fmt.Sprintf("%s adapter received target %s", target, request.Target))
	}
	if request.Operation != "run" {
		return blocked("operation_mismatch", "agent adapters support only canonical run operations")
	}
	return nil
}

func stringPointer(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	copy := value
	return &copy
}

func emitLines(kind adapter.OutputKind, data []byte, nativeID string, emit adapter.Emit) error {
	if emit == nil || len(data) == 0 {
		return nil
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		if err := emit(adapter.Output{Kind: kind, Message: scanner.Text(), NativeSessionID: stringPointer(nativeID)}); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func emitStderr(result CommandResult, nativeID string, emit adapter.Emit) error {
	return emitLines(adapter.OutputStderr, result.Stderr, nativeID, emit)
}

func effectiveDir(request adapter.Request, payloadCWD string) string {
	if strings.TrimSpace(request.CWD) != "" {
		return request.CWD
	}
	if strings.TrimSpace(payloadCWD) != "" {
		return payloadCWD
	}
	return "."
}

var unsafeFilePart = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func safeFilePart(value string) string {
	clean := strings.Trim(unsafeFilePart.ReplaceAllString(value, "-"), "-.")
	if clean == "" {
		sum := sha256.Sum256([]byte(value))
		return hex.EncodeToString(sum[:8])
	}
	return clean
}

func resolveOutputPath(cwd, requested, fallback string) (string, error) {
	if strings.TrimSpace(cwd) == "" {
		cwd = "."
	}
	root, err := filepath.Abs(cwd)
	if err != nil {
		return "", err
	}
	relative := requested
	if strings.TrimSpace(relative) == "" {
		relative = filepath.Join(".hq", "final", fallback)
	}
	if filepath.IsAbs(relative) {
		return "", errors.New("output_path must be relative to cwd")
	}
	destination := filepath.Join(root, filepath.Clean(relative))
	relation, err := filepath.Rel(root, destination)
	if err != nil {
		return "", err
	}
	if relation == ".." || strings.HasPrefix(relation, ".."+string(filepath.Separator)) {
		return "", errors.New("output_path escapes cwd")
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return "", err
	}
	return destination, nil
}

func readNonEmpty(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return "", fmt.Errorf("final output %q is empty", path)
	}
	return text, nil
}

func failure(code, message string, retryable bool) error {
	return &adapter.FailureError{Class: adapter.FailureFailed, Code: code, Message: message, Retryable: retryable}
}

func blocked(code, message string) error {
	return &adapter.FailureError{Class: adapter.FailureBlocked, Code: code, Message: message}
}
