// Command hq-worker-proof-provider is a deterministic executable fixture used
// by lane-L CI. It is copied to provider-shaped filenames and then invoked by
// the real OS runner. It proves process, argv, stdin, session, final-file, and
// readback plumbing; it deliberately does not claim external service access.
package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type invocation struct {
	Version      string   `json:"version"`
	FixtureOnly  bool     `json:"fixture_only"`
	Provider     string   `json:"provider"`
	Argv         []string `json:"argv"`
	StdinBytes   int      `json:"stdin_bytes"`
	StdinSHA256  string   `json:"stdin_sha256"`
	StdinBase64  string   `json:"stdin_base64,omitempty"`
	WorkingDir   string   `json:"working_dir"`
}

func main() {
	stdin, err := io.ReadAll(os.Stdin)
	if err != nil {
		fatal(err)
	}
	provider := providerName(os.Args[0])
	if err := appendInvocation(provider, os.Args[1:], stdin); err != nil {
		fatal(err)
	}
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "version") {
		fmt.Printf("%s-proof 1.0.0\n", provider)
		return
	}

	var runErr error
	switch provider {
	case "sh":
		runErr = runSh(os.Args[1:])
	case "herdr":
		runErr = runHerdr(os.Args[1:])
	case "codex":
		runErr = runCodex(os.Args[1:], stdin)
	case "claude":
		runErr = runClaude(os.Args[1:], stdin)
	default:
		runErr = fmt.Errorf("unknown proof provider filename %q", filepath.Base(os.Args[0]))
	}
	if runErr != nil {
		fatal(runErr)
	}
}

func providerName(path string) string {
	name := strings.TrimSuffix(strings.ToLower(filepath.Base(path)), ".exe")
	for _, candidate := range []string{"herdr", "codex", "claude", "sh"} {
		if strings.Contains(name, candidate) {
			return candidate
		}
	}
	return name
}

func appendInvocation(provider string, argv []string, stdin []byte) error {
	path := strings.TrimSpace(os.Getenv("HQ_PROOF_PROVIDER_LOG"))
	if path == "" {
		return nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	sum := sha256.Sum256(stdin)
	row := invocation{
		Version: "worker.provider-invocation-proof.v1", FixtureOnly: true,
		Provider: provider, Argv: append([]string(nil), argv...), StdinBytes: len(stdin),
		StdinSHA256: "sha256:" + hex.EncodeToString(sum[:]), WorkingDir: cwd,
	}
	if len(stdin) != 0 {
		row.StdinBase64 = base64.StdEncoding.EncodeToString(stdin)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	return json.NewEncoder(file).Encode(row)
}

func runSh(args []string) error {
	if len(args) < 2 || args[0] != "echo" {
		return fmt.Errorf("proof sh expects: echo <literal>")
	}
	fmt.Print(args[1])
	return nil
}

func runHerdr(args []string) error {
	switch {
	case len(args) >= 3 && args[0] == "agent" && args[1] == "start":
		return json.NewEncoder(os.Stdout).Encode(map[string]any{
			"terminal_id": "terminal-proof-1", "pane_id": "pane-proof-1", "name": args[2],
		})
	case len(args) >= 3 && args[0] == "agent" && args[1] == "read":
		fmt.Print("herdr proof output\n")
		return nil
	case len(args) >= 3 && args[0] == "agent" && args[1] == "wait":
		return nil
	case len(args) >= 4 && args[0] == "terminal" && args[1] == "session" && args[2] == "observe":
		return json.NewEncoder(os.Stdout).Encode(map[string]any{
			"type": "terminal.frame", "terminal_id": args[3], "data": "aGVyZHIgcHJvb2Y=",
		})
	default:
		return fmt.Errorf("unsupported Herdr proof argv: %q", args)
	}
}

func runCodex(args []string, stdin []byte) error {
	if len(args) == 0 || args[0] != "exec" {
		return fmt.Errorf("proof Codex requires exec")
	}
	outputPath := valueAfter(args, "--output-last-message")
	if outputPath == "" {
		return fmt.Errorf("Codex proof missing --output-last-message")
	}
	sessionID := "thread-proof-1"
	for index, value := range args {
		if value == "resume" && index+1 < len(args) {
			sessionID = args[index+1]
		}
	}
	if len(args) == 0 || args[len(args)-1] != "-" {
		return fmt.Errorf("Codex proof requires stdin marker")
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return err
	}
	final := "codex proof final: " + strings.TrimSpace(string(stdin))
	if err := os.WriteFile(outputPath, []byte(final+"\n"), 0o600); err != nil {
		return err
	}
	encoder := json.NewEncoder(os.Stdout)
	if err := encoder.Encode(map[string]any{"type": "thread.started", "thread_id": sessionID}); err != nil {
		return err
	}
	return encoder.Encode(map[string]any{"type": "turn.completed"})
}

func runClaude(args []string, stdin []byte) error {
	if !contains(args, "-p") {
		return fmt.Errorf("Claude proof requires -p")
	}
	format := valueAfter(args, "--output-format")
	if format == "" {
		format = "json"
	}
	sessionID := valueAfter(args, "--resume")
	if sessionID == "" {
		sessionID = "session-proof-1"
	}
	result := "claude proof final: " + strings.TrimSpace(string(stdin))
	if format == "stream-json" {
		encoder := json.NewEncoder(os.Stdout)
		if err := encoder.Encode(map[string]any{"type": "assistant", "session_id": sessionID, "message": "proof"}); err != nil {
			return err
		}
		return encoder.Encode(map[string]any{"type": "result", "session_id": sessionID, "result": result, "is_error": false})
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{
		"result": result, "session_id": sessionID, "is_error": false,
	})
}

func valueAfter(values []string, key string) string {
	for index := 0; index+1 < len(values); index++ {
		if values[index] == key {
			return values[index+1]
		}
	}
	return ""
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func fatal(err error) {
	writer := bufio.NewWriter(os.Stderr)
	fmt.Fprintln(writer, err)
	_ = writer.Flush()
	os.Exit(2)
}
