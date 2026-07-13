package hostopen

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"hq/internal/capability"
	"hq/internal/worker/adapter"
)

const (
	helperModeEnv    = "HQ_TEST_HOSTOPEN_HELPER"
	helperMarkerEnv  = "HQ_TEST_HOSTOPEN_MARKER"
	helperCleanupEnv = "HQ_TEST_HOSTOPEN_CLEANUP"
)

func init() {
	if os.Getenv(helperModeEnv) != "handoff" {
		return
	}
	marker := os.Getenv(helperMarkerEnv)
	cleanup := os.Getenv(helperCleanupEnv)
	encoded, err := json.Marshal(os.Args[1:])
	if err != nil || marker == "" || cleanup == "" {
		os.Exit(70)
	}
	if err := os.WriteFile(marker, encoded, 0o600); err != nil {
		os.Exit(71)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(cleanup); err == nil {
			// Model a GUI provider that completes its handoff and later exits
			// non-zero. The host.open adapter must not wait for this status.
			os.Exit(23)
		}
		time.Sleep(10 * time.Millisecond)
	}
	os.Exit(24)
}

func TestRunStartFailureRemainsProviderFailureBeforeLaunch(t *testing.T) {
	a := newTestAdapter(t)
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "provider-started.json")
	cleanup := filepath.Join(root, "provider-cleanup")
	t.Setenv(helperModeEnv, "handoff")
	t.Setenv(helperMarkerEnv, marker)
	t.Setenv(helperCleanupEnv, cleanup)
	t.Setenv("PATH", t.TempDir())
	t.Cleanup(func() { _ = os.WriteFile(cleanup, nil, 0o600) })

	completion, err := a.Run(context.Background(), testRequest(t, target, filepath.Join(root, "missing-cwd")), nil)
	if err == nil {
		t.Fatal("expected provider start failure")
	}
	if completion.FinalText != "" || completion.FinalPath != "" || completion.NativeSessionID != nil {
		t.Fatalf("unexpected completion after start failure: %+v", completion)
	}
	var failure *adapter.FailureError
	if !errors.As(err, &failure) {
		t.Fatalf("error type=%T want *adapter.FailureError: %v", err, err)
	}
	if failure.Class != adapter.FailureFailed || failure.Code != "provider_failed" {
		t.Fatalf("failure=%+v", failure)
	}
	if _, statErr := os.Stat(marker); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("provider launched despite Start failure: %v", statErr)
	}
}

func TestRunCompletesAfterSuccessfulReleaseAndHandoff(t *testing.T) {
	a := newTestAdapter(t)
	root := t.TempDir()
	target := filepath.Join(root, "target with spaces")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	requestPath := filepath.ToSlash(target)
	cleanPath := filepath.Clean(requestPath)
	marker := filepath.Join(root, "provider-started.json")
	cleanup := filepath.Join(root, "provider-cleanup")
	t.Setenv(helperModeEnv, "handoff")
	t.Setenv(helperMarkerEnv, marker)
	t.Setenv(helperCleanupEnv, cleanup)
	t.Setenv("PATH", t.TempDir())
	t.Cleanup(func() { _ = os.WriteFile(cleanup, nil, 0o600) })
	request := testRequest(t, requestPath, root)

	type outcome struct {
		completion adapter.Completion
		err        error
	}
	done := make(chan outcome, 1)
	started := time.Now()
	go func() {
		completion, err := a.Run(context.Background(), request, nil)
		done <- outcome{completion: completion, err: err}
	}()

	var got outcome
	select {
	case got = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("host.open waited for provider exit instead of releasing after launch")
	}
	if got.err != nil {
		t.Fatalf("Run() error=%v", got.err)
	}
	if elapsed := time.Since(started); elapsed >= 2*time.Second {
		t.Fatalf("launch handoff took %v", elapsed)
	}
	if got.completion.FinalText != "host.open launch handed off via test-host-provider" || got.completion.FinalPath != cleanPath {
		t.Fatalf("completion=%+v", got.completion)
	}

	var markerBytes []byte
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var err error
		markerBytes, err = os.ReadFile(marker)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if markerBytes == nil {
		t.Fatal("released provider did not complete its launch handoff")
	}
	var args []string
	if err := json.Unmarshal(markerBytes, &args); err != nil {
		t.Fatal(err)
	}
	if want := []string{cleanPath}; !reflect.DeepEqual(args, want) {
		t.Fatalf("provider args=%q want=%q", args, want)
	}
	if err := os.WriteFile(cleanup, nil, 0o600); err != nil {
		t.Fatal(err)
	}
}

func newTestAdapter(t *testing.T) *Adapter {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	providerBytes, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(providerBytes)
	a, err := New(capability.Binding{
		Kind:               capability.BindingKind,
		CapabilityID:       capability.HostOpenCapability,
		ProviderID:         "test-host-provider",
		ContractVersion:    capability.HostOpenContract,
		DeploymentID:       "dep-test",
		ProviderKind:       "executable",
		ExecutablePath:     executable,
		IntegrityDigest:    "sha256:" + hex.EncodeToString(digest[:]),
		VerificationStatus: "verified",
	})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func testRequest(t *testing.T, path, cwd string) adapter.Request {
	t.Helper()
	payload, err := json.Marshal(map[string]string{
		"capability": capability.HostOpenCapability,
		"path":       path,
	})
	if err != nil {
		t.Fatal(err)
	}
	return adapter.Request{
		RunID:         "run-host-open-test",
		InstructionID: "ins-host-open-test",
		Target:        "host",
		Operation:     "run",
		Payload:       payload,
		CWD:           cwd,
	}
}
