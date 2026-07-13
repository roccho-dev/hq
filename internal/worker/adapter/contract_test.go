package adapter

import (
	"context"
	"encoding/json"
	"errors"
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

func TestRegistryPreparesDynamicProviderWithoutExecutingIt(t *testing.T) {
	adapterCalls := 0
	prepareCalls := 0
	descriptor := ProviderDescriptor{
		CapabilityID: "bin.test", ProviderID: "binding.test", ContractVersion: "local-tool.exec.v1",
		DeploymentID: "dep-1", ProviderKind: "executable", IntegrityDigest: "sha256:" + strings.Repeat("a", 64),
	}
	registry, err := NewRegistry(Registration{Target: "sh", Preparer: PreparerFunc(func(_ context.Context, request Request) (Prepared, error) {
		prepareCalls++
		if request.InstructionID != "ins-1" {
			t.Fatalf("request=%+v", request)
		}
		return Prepared{Adapter: fakeAdapter{calls: &adapterCalls}, Provider: &descriptor}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Resolve("sh"); err == nil || !strings.Contains(err.Error(), "requires request preparation") {
		t.Fatalf("resolve error=%v", err)
	}
	registration, err := registry.ResolveRegistration("sh")
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := registration.Prepare(context.Background(), Request{
		RunID: "run-1", InstructionID: "ins-1", Target: "sh", Operation: "run", Payload: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if prepareCalls != 1 || adapterCalls != 0 || prepared.Provider == nil || !prepared.Provider.Equal(descriptor) {
		t.Fatalf("prepare=%d adapter=%d prepared=%+v", prepareCalls, adapterCalls, prepared)
	}
}

func TestRegistryRequiresExactlyOneStaticOrDynamicImplementation(t *testing.T) {
	calls := 0
	preparer := PreparerFunc(func(context.Context, Request) (Prepared, error) { return Prepared{}, nil })
	provider := ProviderDescriptor{
		CapabilityID: "bin.test", ProviderID: "binding.test", ContractVersion: "local-tool.exec.v1",
		DeploymentID: "dep-1", ProviderKind: "executable", IntegrityDigest: "sha256:" + strings.Repeat("a", 64),
	}
	for _, registration := range []Registration{
		{Target: "sh"},
		{Target: "sh", Adapter: fakeAdapter{calls: &calls}, Preparer: preparer},
	} {
		if _, err := NewRegistry(registration); err == nil || !strings.Contains(err.Error(), "exactly one") {
			t.Fatalf("registration=%+v error=%v", registration, err)
		}
	}
	if _, err := NewRegistry(Registration{Target: "sh", Preparer: preparer, Provider: &provider}); err == nil || !strings.Contains(err.Error(), "during preparation") {
		t.Fatalf("dynamic provider error=%v", err)
	}
}

func TestDynamicPreparationMustReturnValidAdapterAndProvider(t *testing.T) {
	for _, test := range []struct {
		name     string
		prepared Prepared
	}{
		{name: "nil adapter", prepared: Prepared{Provider: &ProviderDescriptor{}}},
		{name: "nil provider", prepared: Prepared{Adapter: fakeAdapter{calls: new(int)}}},
		{name: "invalid provider", prepared: Prepared{Adapter: fakeAdapter{calls: new(int)}, Provider: &ProviderDescriptor{}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			registry, err := NewRegistry(Registration{Target: "sh", Preparer: PreparerFunc(func(context.Context, Request) (Prepared, error) {
				return test.prepared, nil
			})})
			if err != nil {
				t.Fatal(err)
			}
			registration, err := registry.ResolveRegistration("sh")
			if err != nil {
				t.Fatal(err)
			}
			_, err = registration.Prepare(context.Background(), Request{})
			var failure *FailureError
			if !errors.As(err, &failure) || failure.Code != "provider_prepare_invalid" {
				t.Fatalf("error=%T %v", err, err)
			}
		})
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
