package workerclaim

import (
	"sync"
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

func TestHeartbeatReadersNeverObserveATruncatedUpdate(t *testing.T) {
	root := t.TempDir()
	base := time.Date(2026, 7, 14, 7, 0, 0, 0, time.UTC)
	claim, err := Acquire(root, "worker-atomic-heartbeat", base)
	if err != nil {
		t.Fatal(err)
	}
	defer claim.Release()
	if _, err := claim.WriteHeartbeat("dep-atomic", "local", "configured_ready", base); err != nil {
		t.Fatal(err)
	}

	const updates = 100
	start := make(chan struct{})
	errors := make(chan error, updates)
	var readers sync.WaitGroup
	readers.Add(1)
	go func() {
		defer readers.Done()
		<-start
		for index := 0; index < updates; index++ {
			if _, err := ReadHeartbeat(root); err != nil {
				errors <- err
				return
			}
		}
	}()
	close(start)
	for index := 1; index <= updates; index++ {
		if _, err := claim.WriteHeartbeat("dep-atomic", "local", "configured_ready", base.Add(time.Duration(index)*time.Millisecond)); err != nil {
			t.Fatal(err)
		}
	}
	readers.Wait()
	close(errors)
	for err := range errors {
		t.Fatalf("heartbeat reader observed a partial update: %v", err)
	}
}
