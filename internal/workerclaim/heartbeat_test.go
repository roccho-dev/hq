package workerclaim

import (
	"testing"
	"time"
)

func TestManagedHeartbeatBlocksLiveRecoveryAndAllowsExactStaleRecovery(t *testing.T) {
	root := t.TempDir()
	base := time.Date(2026, 7, 10, 7, 0, 0, 0, time.UTC)
	claim, err := Acquire(root, "worker-a", base)
	if err != nil {
		t.Fatal(err)
	}
	owner := claim.Owner()
	if _, err := claim.WriteHeartbeat("dep-1", "local", "configured_ready", base); err != nil {
		t.Fatal(err)
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
