package adapter

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

type fakeAdapter struct{ calls *int }

func (f fakeAdapter) Run(_ context.Context, _ Request, emit Emit) (Completion, error) {
	(*f.calls)++
	if err := emit(Output{Kind: OutputStdout, Message: "output"}); err != nil {
		return Completion{}, err
	}
	return Completion{FinalText: "done"}, nil
}

func TestRegistryResolvesOnlyExplicitCanonicalTargets(t *testing.T) {
	calls := 0
	registry, err := NewRegistry(
		Registration{Target: "sh", Adapter: fakeAdapter{calls: &calls}},
		Registration{Target: "herdr", Adapter: fakeAdapter{calls: &calls}},
		Registration{Target: "codex", Adapter: fakeAdapter{calls: &calls}},
		Registration{Target: "claude", Adapter: fakeAdapter{calls: &calls}},
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"sh", "herdr", "codex", "claude"} {
		if _, err := registry.Resolve(target); err != nil {
			t.Fatalf("resolve %s: %v", target, err)
		}
	}
	for _, target := range []string{"shell", "SH", " sh", "unknown"} {
		if _, err := registry.Resolve(target); err == nil {
			t.Fatalf("target %q unexpectedly resolved", target)
		}
	}
	if calls != 0 {
		t.Fatalf("resolve must not execute adapters; calls=%d", calls)
	}
	want := RegistrySnapshot{Version: RegistryVersion, Targets: []string{"claude", "codex", "herdr", "sh"}}
	if got := registry.Snapshot(); !reflect.DeepEqual(got, want) {
		t.Fatalf("snapshot mismatch: got %#v want %#v", got, want)
	}
}

func TestRegistryRejectsDuplicatesAliasesAndUnavailableTargets(t *testing.T) {
	calls := 0
	_, err := NewRegistry(
		Registration{Target: "sh", Adapter: fakeAdapter{calls: &calls}},
		Registration{Target: "sh", Adapter: fakeAdapter{calls: &calls}},
	)
	if err == nil || !strings.Contains(err.Error(), "registered twice") {
		t.Fatalf("expected duplicate rejection, got %v", err)
	}
	_, err = NewRegistry(Registration{Target: "shell", Adapter: fakeAdapter{calls: &calls}})
	if err == nil || !strings.Contains(err.Error(), "not part") {
		t.Fatalf("expected alias rejection, got %v", err)
	}
	registry, err := NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Resolve("sh"); err == nil || !strings.Contains(err.Error(), "no adapter") {
		t.Fatalf("expected unavailable adapter, got %v", err)
	}
}

func TestTransientAdapterDataCannotCarryDurableEnvelopeFields(t *testing.T) {
	encoded, err := json.Marshal(struct {
		Request    Request
		Output     Output
		Completion Completion
	}{
		Request:    Request{RunID: "run-1", InstructionID: "ins-1", Target: "sh", Operation: "run", Payload: json.RawMessage(`{}`)},
		Output:     Output{Kind: OutputStdout, Message: "hello"},
		Completion: Completion{FinalText: "done"},
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, forbidden := range []string{"event_id", "seq", "recorded_at", "result.v1", "adapter.event.v1"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("transient adapter data contains durable field %q: %s", forbidden, text)
		}
	}
}

func TestCompletionRequiresARealFinalAnswer(t *testing.T) {
	if err := (Completion{}).Validate(); err == nil {
		t.Fatal("empty completion must not become completed result.v1")
	}
}
