package workerclaim

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hq/internal/core"
)

func TestManagedHeartbeatBlocksLiveRecoveryAndAllowsExactStaleRecovery(t *testing.T) {
	root := t.TempDir()
	base := time.Date(2026, 7, 10, 7, 0, 0, 0, time.UTC)
	claim, err := Acquire(root, "worker-a", base)
	if err != nil {
		t.Fatal(err)
	}
	owner := claim.Owner()
	world := core.WorldRef{WorldID: "world.test", Digest: "sha256:" + strings.Repeat("0", 64)}
	written, err := claim.WriteHeartbeat("dep-1", "local", "configured_ready", world, base)
	if err != nil {
		t.Fatal(err)
	}
	if written.Kind != HeartbeatKindV2 || written.SelectedWorld == nil || *written.SelectedWorld != world {
		t.Fatalf("heartbeat=%+v", written)
	}
	if _, fresh, err := HeartbeatFresh(root, owner.ClaimID, base.Add(time.Second), 10*time.Second); err != nil || !fresh {
		t.Fatalf("fresh=%v err=%v", fresh, err)
	}
	if _, err := RecoverManaged(root, owner.ClaimID, "must not steal live worker", base.Add(time.Second), 10*time.Second); err == nil {
		t.Fatal("fresh managed worker recovery must fail")
	}
	if _, err := RecoverManaged(root, owner.ClaimID, "operator verified stale worker", base.Add(time.Minute), 10*time.Second); err != nil {
		t.Fatal(err)
	}
	inspection, err := Inspect(root, base.Add(time.Minute), 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Exists {
		t.Fatal("stale claim must be removed after exact recovery")
	}
}

func TestLegacyHeartbeatIsReadOnlyCompatibilityEvidenceForBoundedRecovery(t *testing.T) {
	root := t.TempDir()
	base := time.Date(2026, 7, 10, 8, 0, 0, 0, time.UTC)
	claim, err := Acquire(root, "worker-v1", base)
	if err != nil {
		t.Fatal(err)
	}
	owner := claim.Owner()
	legacy := Heartbeat{
		Kind: HeartbeatKindV1, ClaimID: owner.ClaimID, WorkerID: owner.WorkerID,
		Workspace: owner.Workspace, DeploymentID: "dep-v1", Profile: "local",
		State: "configured_ready", ObservedAt: base,
	}
	writeHeartbeatFixture(t, root, legacy)
	read, fresh, err := HeartbeatFresh(root, owner.ClaimID, base.Add(time.Second), 10*time.Second)
	if err != nil || !fresh || read.Kind != HeartbeatKindV1 || read.SelectedWorld != nil {
		t.Fatalf("read=%+v fresh=%v err=%v", read, fresh, err)
	}
	if _, err := RecoverManaged(root, owner.ClaimID, "fresh legacy evidence", base.Add(time.Second), 10*time.Second); err == nil {
		t.Fatal("fresh legacy heartbeat must continue to bound recovery")
	}
	if _, err := RecoverManaged(root, owner.ClaimID, "stale legacy evidence", base.Add(time.Minute), 10*time.Second); err != nil {
		t.Fatal(err)
	}
}

func TestHeartbeatV2RequiresValidSelectedWorld(t *testing.T) {
	root := t.TempDir()
	base := time.Date(2026, 7, 10, 9, 0, 0, 0, time.UTC)
	claim, err := Acquire(root, "worker-invalid", base)
	if err != nil {
		t.Fatal(err)
	}
	owner := claim.Owner()
	invalid := Heartbeat{
		Kind: HeartbeatKindV2, ClaimID: owner.ClaimID, WorkerID: owner.WorkerID,
		Workspace: owner.Workspace, DeploymentID: "dep", Profile: "local",
		State: "configured_ready", ObservedAt: base,
	}
	writeHeartbeatFixture(t, root, invalid)
	if _, err := ReadHeartbeat(root); err == nil {
		t.Fatal("v2 heartbeat without selected_world was accepted")
	}
}

func TestHeartbeatAtomicReplacementSupportsConcurrentReaders(t *testing.T) {
	root := t.TempDir()
	base := time.Date(2026, 7, 10, 10, 0, 0, 0, time.UTC)
	claim, err := Acquire(root, "worker-concurrent", base)
	if err != nil {
		t.Fatal(err)
	}
	worlds := []core.WorldRef{
		{WorldID: "world.concurrent-a", Digest: "sha256:" + strings.Repeat("0", 64)},
		{WorldID: "world.concurrent-b", Digest: "sha256:" + strings.Repeat("1", 64)},
	}
	if _, err := claim.WriteHeartbeat("dep-concurrent", "local", "configured_ready", worlds[0], base); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		for i := 1; i <= 50; i++ {
			if _, err := claim.WriteHeartbeat("dep-concurrent", "local", "configured_ready", worlds[i%len(worlds)], base.Add(time.Duration(i)*time.Millisecond)); err != nil {
				result <- fmt.Errorf("write %d: %w", i, err)
				return
			}
		}
		result <- nil
	}()
	reads := 0
	for {
		select {
		case err := <-result:
			if err != nil {
				t.Fatal(err)
			}
			if reads == 0 {
				t.Fatal("stress proof completed without a concurrent read")
			}
			final, err := ReadHeartbeat(root)
			if err != nil || final.Kind != HeartbeatKindV2 || final.SelectedWorld == nil {
				t.Fatalf("final heartbeat=%+v err=%v", final, err)
			}
			if temporary, err := filepath.Glob(filepath.Join(root, ".hq", "worker", ".heartbeat-*.tmp")); err != nil || len(temporary) != 0 {
				t.Fatalf("temporary heartbeat files=%v err=%v", temporary, err)
			}
			return
		default:
			record, err := ReadHeartbeat(root)
			if err != nil {
				t.Fatalf("concurrent read %d: %v", reads, err)
			}
			if record.Kind != HeartbeatKindV2 || record.SelectedWorld == nil || (*record.SelectedWorld != worlds[0] && *record.SelectedWorld != worlds[1]) {
				t.Fatalf("concurrent heartbeat=%+v", record)
			}
			reads++
		}
	}
}

func writeHeartbeatFixture(t *testing.T, root string, heartbeat Heartbeat) {
	t.Helper()
	data, err := json.Marshal(heartbeat)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".hq", "worker", heartbeatName)
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}
