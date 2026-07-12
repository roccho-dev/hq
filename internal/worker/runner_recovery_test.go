package worker

import (
	"context"
	"strings"
	"testing"
	"time"

	"hq/internal/worker/adapter"
)

type recoveryAdapter struct {
	calls int
	key   string
}

func (a *recoveryAdapter) Run(_ context.Context, request adapter.Request, _ adapter.Emit) (adapter.Completion, error) {
	a.calls++
	a.key = request.IdempotencyKey
	return adapter.Completion{FinalText: "recovered"}, nil
}

func runningEvidence(row ReadRow, provider *ProviderEvidence) LogData {
	at := time.Date(2026, 7, 10, 7, 0, 0, 0, time.UTC)
	return LogData{Results: []ResultRow{
		{EventID: "evt-recovery-0", Version: ResultVersionV1, RunID: "run-recovery", InstructionID: row.Instruction.ID, Target: row.Instruction.Target, Kind: ResultAccepted, Seq: 0, RecordedAt: at},
		{EventID: "evt-recovery-1", Version: ResultVersionV1, RunID: "run-recovery", InstructionID: row.Instruction.ID, Target: row.Instruction.Target, Kind: ResultStarted, Seq: 1, RecordedAt: at.Add(time.Second), Provider: provider},
	}}
}

func TestRunnerAmbiguousStartedRunFailsClosedWithoutProviderRetry(t *testing.T) {
	row := runnerRow(t, "plain")
	fake := &recoveryAdapter{}
	registry, err := adapter.NewRegistry(adapter.Registration{Target: "sh", Adapter: fake})
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(t.TempDir(), registry, approvedRunnerFixture(t, row.Instruction))
	sink := &memoryAppender{}
	emitted, unsuccessful, err := runner.Process(context.Background(), []ReadRow{row}, runningEvidence(row, nil), false, sink)
	if err != nil {
		t.Fatal(err)
	}
	if fake.calls != 0 || unsuccessful != 1 {
		t.Fatalf("calls=%d unsuccessful=%d", fake.calls, unsuccessful)
	}
	if len(emitted) != 2 || emitted[1].Result == nil || emitted[1].Result.Kind != ResultBlocked || emitted[1].Result.Error == nil || emitted[1].Result.Error.Code != "reconcile_required" {
		t.Fatalf("emitted=%+v", emitted)
	}
}

func TestRunnerResumesOnlySameVerifiedIdempotentProviderAndKey(t *testing.T) {
	row := runnerRow(t, "plain")
	fake := &recoveryAdapter{}
	descriptor := adapter.ProviderDescriptor{
		CapabilityID: "test.exec", ProviderID: "provider-a", ContractVersion: "test.exec.v1",
		DeploymentID: "dep-1", ProviderKind: "executable", IntegrityDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		IdempotencyContract: "test.exec.idempotency.v1",
	}
	registry, err := adapter.NewRegistry(adapter.Registration{Target: "sh", Adapter: fake, Provider: &descriptor})
	if err != nil {
		t.Fatal(err)
	}
	provider := &ProviderEvidence{
		CapabilityID: descriptor.CapabilityID, ProviderID: descriptor.ProviderID, ContractVersion: descriptor.ContractVersion,
		DeploymentID: descriptor.DeploymentID, ProviderKind: descriptor.ProviderKind, IntegrityDigest: descriptor.IntegrityDigest,
		IdempotencyContract: descriptor.IdempotencyContract, IdempotencyKey: "stable-key",
	}
	runner := NewRunner(t.TempDir(), registry, approvedRunnerFixture(t, row.Instruction))
	sink := &memoryAppender{}
	emitted, unsuccessful, err := runner.Process(context.Background(), []ReadRow{row}, runningEvidence(row, provider), false, sink)
	if err != nil {
		t.Fatal(err)
	}
	if unsuccessful != 0 || fake.calls != 1 || fake.key != "stable-key" {
		t.Fatalf("unsuccessful=%d calls=%d key=%q", unsuccessful, fake.calls, fake.key)
	}
	if len(emitted) != 2 || emitted[0].Policy == nil || emitted[1].Result == nil || emitted[1].Result.Kind != ResultCompleted {
		t.Fatalf("emitted=%+v", emitted)
	}
}

func TestRunnerRecoveryRepreparesAndRequiresSameDynamicProvider(t *testing.T) {
	row := runnerRow(t, "plain")
	priorDescriptor := adapter.ProviderDescriptor{
		CapabilityID: "bin.test", ProviderID: "binding.test", ContractVersion: "local-tool.exec.v1",
		DeploymentID: "dep-1", ProviderKind: "executable", IntegrityDigest: "sha256:" + strings.Repeat("a", 64),
		IdempotencyContract: "local-tool.exec.idempotency.v1",
	}
	priorProvider := &ProviderEvidence{
		CapabilityID: priorDescriptor.CapabilityID, ProviderID: priorDescriptor.ProviderID, ContractVersion: priorDescriptor.ContractVersion,
		DeploymentID: priorDescriptor.DeploymentID, ProviderKind: priorDescriptor.ProviderKind, IntegrityDigest: priorDescriptor.IntegrityDigest,
		IdempotencyContract: priorDescriptor.IdempotencyContract, IdempotencyKey: "stable-key",
	}
	for _, test := range []struct {
		name        string
		descriptor  adapter.ProviderDescriptor
		wantCalls   int
		wantFailure bool
	}{
		{name: "same binding resumes", descriptor: priorDescriptor, wantCalls: 1},
		{name: "changed digest reconciles", descriptor: func() adapter.ProviderDescriptor {
			changed := priorDescriptor
			changed.IntegrityDigest = "sha256:" + strings.Repeat("b", 64)
			return changed
		}(), wantFailure: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := &recoveryAdapter{}
			prepareCalls := 0
			registry, err := adapter.NewRegistry(adapter.Registration{Target: "sh", Preparer: adapter.PreparerFunc(func(context.Context, adapter.Request) (adapter.Prepared, error) {
				prepareCalls++
				return adapter.Prepared{Adapter: fake, Provider: &test.descriptor}, nil
			})})
			if err != nil {
				t.Fatal(err)
			}
			runner := NewRunner(t.TempDir(), registry, approvedRunnerFixture(t, row.Instruction))
			sink := &memoryAppender{}
			emitted, unsuccessful, err := runner.Process(context.Background(), []ReadRow{row}, runningEvidence(row, priorProvider), false, sink)
			if err != nil {
				t.Fatal(err)
			}
			if prepareCalls != 1 || fake.calls != test.wantCalls {
				t.Fatalf("prepare=%d adapter=%d", prepareCalls, fake.calls)
			}
			last := emitted[len(emitted)-1].Result
			if test.wantFailure {
				if unsuccessful != 1 || last == nil || last.Kind != ResultBlocked || last.Error == nil || last.Error.Code != "reconcile_required" {
					t.Fatalf("unsuccessful=%d emitted=%+v", unsuccessful, emitted)
				}
			} else if unsuccessful != 0 || fake.key != "stable-key" || last == nil || last.Kind != ResultCompleted {
				t.Fatalf("unsuccessful=%d key=%q emitted=%+v", unsuccessful, fake.key, emitted)
			}
		})
	}
}
