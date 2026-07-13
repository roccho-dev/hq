package hostopen

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
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
	helperDoneEnv    = "HQ_TEST_HOSTOPEN_DONE"
)

func init() {
	if os.Getenv(helperModeEnv) != "handoff" {
		return
	}
	marker := os.Getenv(helperMarkerEnv)
	cleanup := os.Getenv(helperCleanupEnv)
	done := os.Getenv(helperDoneEnv)
	encoded, err := json.Marshal(os.Args[1:])
	if err != nil || marker == "" || cleanup == "" || done == "" {
		os.Exit(70)
	}
	if err := os.WriteFile(marker, encoded, 0o600); err != nil {
		os.Exit(71)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(cleanup); err == nil {
			// Model a GUI provider that completed handoff and later exits
			// non-zero. host.open must not wait for this status.
			_ = os.WriteFile(done, []byte("exiting-23"), 0o600)
			os.Exit(23)
		}
		time.Sleep(10 * time.Millisecond)
	}
	_ = os.WriteFile(done, []byte("exiting-24"), 0o600)
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
	done := filepath.Join(root, "provider-done")
	t.Setenv(helperModeEnv, "handoff")
	t.Setenv(helperMarkerEnv, marker)
	t.Setenv(helperCleanupEnv, cleanup)
	t.Setenv(helperDoneEnv, done)
	t.Setenv("PATH", t.TempDir())

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
	root := t.TempDir()
	helperExecutable := copyTestExecutable(t, root)
	a := newTestAdapterForExecutable(t, helperExecutable)
	target := filepath.Join(root, "target with spaces")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	requestPath := filepath.ToSlash(target)
	cleanPath := filepath.Clean(requestPath)
	marker := filepath.Join(root, "provider-started.json")
	cleanup := filepath.Join(root, "provider-cleanup")
	done := filepath.Join(root, "provider-done")
	t.Setenv(helperModeEnv, "handoff")
	t.Setenv(helperMarkerEnv, marker)
	t.Setenv(helperCleanupEnv, cleanup)
	t.Setenv(helperDoneEnv, done)
	t.Setenv("PATH", t.TempDir())
	request := testRequest(t, requestPath, root)

	type outcome struct {
		completion adapter.Completion
		err        error
	}
	result := make(chan outcome, 1)
	go func() {
		completion, err := a.Run(context.Background(), request, nil)
		result <- outcome{completion: completion, err: err}
	}()

	var got outcome
	select {
	case got = <-result:
	case <-time.After(2 * time.Second):
		t.Fatal("host.open waited for provider exit instead of releasing after launch")
	}
	if got.err != nil {
		t.Fatalf("Run() error=%v", got.err)
	}
	if got.completion.FinalText != "host.open launch handed off via test-host-provider" || got.completion.FinalPath != cleanPath {
		t.Fatalf("completion=%+v", got.completion)
	}

	args := waitReadJSONStrings(t, marker, 2*time.Second)
	if want := []string{cleanPath}; !reflect.DeepEqual(args, want) {
		t.Fatalf("provider args=%q want=%q", args, want)
	}
	if err := os.WriteFile(cleanup, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	waitReadExactText(t, done, "exiting-23", 2*time.Second)
	waitRemoveFile(t, helperExecutable, 2*time.Second)
}

func TestRunRejectsCancellationBeforeEffect(t *testing.T) {
	a := newTestAdapter(t)
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "provider-started.json")
	cleanup := filepath.Join(root, "provider-cleanup")
	done := filepath.Join(root, "provider-done")
	t.Setenv(helperModeEnv, "handoff")
	t.Setenv(helperMarkerEnv, marker)
	t.Setenv(helperCleanupEnv, cleanup)
	t.Setenv(helperDoneEnv, done)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.Run(ctx, testRequest(t, target, root), nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v want context.Canceled", err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("provider launched after pre-effect cancellation: %v", err)
	}
}

func newTestAdapter(t *testing.T) *Adapter {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return newTestAdapterForExecutable(t, executable)
}

func newTestAdapterForExecutable(t *testing.T, executable string) *Adapter {
	t.Helper()
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

func copyTestExecutable(t *testing.T, root string) string {
	t.Helper()
	source, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root, "hostopen-helper"+filepath.Ext(source))
	input, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o700)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(output, input); err != nil {
		_ = output.Close()
		t.Fatal(err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
	return destination
}

func waitReadJSONStrings(t *testing.T, path string, timeout time.Duration) []string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			var values []string
			if json.Unmarshal(data, &values) == nil {
				return values
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for complete JSON in %s", path)
	return nil
}

func waitReadExactText(t *testing.T, path, expected string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			if string(data) == expected {
				return
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q in %s", expected, path)
}

func waitRemoveFile(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		err := os.Remove(path)
		if err == nil || errors.Is(err, os.ErrNotExist) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for helper exit: %s", path)
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
