package workersafety

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type redactionFixture struct {
	Input          map[string]any `json:"input"`
	MustNotContain []string       `json:"must_not_contain"`
	MustContain    []string       `json:"must_contain"`
}

func TestRedactRecordMasksDurableLogSecretsAndPreservesStructure(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", "spec", "fixtures", "worker-safety", "redaction.json"))
	if err != nil {
		t.Fatal(err)
	}
	tokens := map[string]string{
		"{{ENV_TOKEN}}":      strings.Repeat("env-secret-", 3),
		"{{BEARER_TOKEN}}":   strings.Repeat("a", 26),
		"{{OPENAI_TOKEN}}":   "sk-" + strings.Repeat("b", 24),
		"{{GITHUB_TOKEN}}":   "ghp_" + strings.Repeat("c", 32),
		"{{AWS_ACCESS_KEY}}": "AKIA" + strings.Repeat("D", 16),
		"{{CLIENT_SECRET}}":  strings.Repeat("client-secret-", 2),
	}
	for marker, value := range tokens {
		content = []byte(strings.ReplaceAll(string(content), marker, value))
	}

	var fixture redactionFixture
	if err := json.Unmarshal(content, &fixture); err != nil {
		t.Fatal(err)
	}

	redacted, changed := RedactRecord(fixture.Input)
	if !changed {
		t.Fatal("expected redaction change")
	}
	encoded, err := json.Marshal(redacted)
	if err != nil {
		t.Fatalf("redacted record must remain valid JSON: %v", err)
	}
	text := string(encoded)
	for _, secret := range fixture.MustNotContain {
		if strings.Contains(text, secret) {
			t.Fatalf("secret %q leaked in %s", secret, text)
		}
	}
	for _, required := range fixture.MustContain {
		if !strings.Contains(text, required) {
			t.Fatalf("required debugging field %q was removed: %s", required, text)
		}
	}
	if fixture.Input["stdout"] == redacted["stdout"] {
		t.Fatal("stdout secret should be changed")
	}
	if fixture.Input["run_id"] != redacted["run_id"] {
		t.Fatal("run_id must remain stable")
	}
}

func TestRedactValueHandlesPrivateKeyAndStringMap(t *testing.T) {
	pem := "-----BEGIN PRIVATE KEY-----\nabc123\n-----END PRIVATE KEY-----"
	value := map[string]string{
		"message":  "key=" + pem,
		"password": "plain-text",
		"status":   "failed",
	}
	redactedAny, changed := RedactValue(value)
	if !changed {
		t.Fatal("expected redaction")
	}
	redacted := redactedAny.(map[string]string)
	if strings.Contains(redacted["message"], "abc123") {
		t.Fatal("private key body leaked")
	}
	if redacted["password"] != Redacted {
		t.Fatalf("password key was not fully redacted: %q", redacted["password"])
	}
	if redacted["status"] != "failed" {
		t.Fatalf("status changed: %q", redacted["status"])
	}
}

func TestRedactRecordDoesNotMutateInputOrOverRedactNormalFields(t *testing.T) {
	input := map[string]any{
		"run_id": "run-002",
		"status": "running",
		"payload": map[string]any{
			"note": "normal output without credentials",
		},
	}
	redacted, changed := RedactRecord(input)
	if changed {
		t.Fatalf("normal record should not change: %#v", redacted)
	}
	redacted["status"] = "changed"
	if input["status"] != "running" {
		t.Fatal("input was mutated")
	}
}
