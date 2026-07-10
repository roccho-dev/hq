package worker

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestFollowRunUnknownRunFailsImmediatelyInFollowMode(t *testing.T) {
	started := time.Now()
	err := FollowRun(
		context.Background(),
		filepath.Join(t.TempDir(), "missing.jsonl"),
		"missing",
		true,
		time.Hour,
		func(ResultRow) error { return nil },
	)
	var observationError *ObservationError
	if !errors.As(err, &observationError) || observationError.Code != "run_not_found" {
		t.Fatalf("err=%T %v", err, err)
	}
	if time.Since(started) > time.Second {
		t.Fatalf("unknown run did not fail immediately")
	}
}
