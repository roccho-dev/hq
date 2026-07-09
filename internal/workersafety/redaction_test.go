package workersafety

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRedactRecordMasksDurableLogSecretsAndPreservesStructure(t *testing.T) {
	input := map[string]any{
		"kind":              "worker.event.v1",
		"run_id":            "run-001",
		"status":            "completed",
		"native_session_id": "session-001",
		"env": map[string]any{
			"API_TOKEN": "super-secret-token",
			"MODE":      "test",
		},
		"payload": map[string]any{
			"authorization": "Bearer abcdefghijklmnopqrstuvwxyz",
			"prompt":        "use sk-abcdefghijklmnopqrstuvwxyz safely",
		},
		"stdout": "connected with ghp_abcdefghijklmnopqrstuvwxyz123456",
		"stderr": "AWS_ACCESS_KEY_ID=AKIA1234567890ABCDEF",
		"final": map[string]any{
			"final_path":   ".hq/outputs/run-001/final.json",
			"clientSecret": "do-not-store-this",
		},
	}

	redacted, changed := RedactRecord(input)
	if !changed {
		t.Fatal("expected redaction change")
	}
	encoded, err := json.Marshal(redacted)
	if err != nil {
		t.Fatalf("redacted record must remain valid JSON: %v", err)
	}
	text := string(encoded)
	for _, secret := range []string{
		"super-secret-token",
		"abcdefghijklmnopqrstuvwxyz",
		"ghp_abcdefghijklmnopqrstuvwxyz123456",
		"AKIA1234567890ABCDEF",
		"do-not-store-this",
	} {
		if strings.Contains(text, secret) {
			t.Fatalf("secret %q leaked in %s", secret, text)
		}
	}
	for _, required := range []string{"run-001", "completed", "session-001", ".hq/outputs/run-001/final.json"} {
		if !strings.Contains(text, required) {
			t.Fatalf("required debugging field %q was removed: %s", required, text)
		}
	}
	if input["stdout"] == redacted["stdout"] {
		t.Fatal("stdout secret should be changed")
	}
	if input["run_id"] != redacted["run_id"] {
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
