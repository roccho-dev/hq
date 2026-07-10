package runner

import (
	"fmt"
	"time"

	"github.com/roccho-dev/ops/packages/hq/internal/hostopen"
	"github.com/roccho-dev/ops/packages/hq/internal/profile"
	"github.com/roccho-dev/ops/packages/hq/internal/queue"
)

type Result struct {
	Processed int            `json:"processed"`
	Receipt   *queue.Receipt `json:"receipt,omitempty"`
}

func RunOnce(p *profile.Profile, opener hostopen.Opener) (Result, error) {
	rows, err := queue.ReadRows(p.QueuePath)
	if err != nil {
		return Result{}, err
	}
	processed, err := queue.ReadReceiptQueueIDs(p.ReceiptPath)
	if err != nil {
		return Result{}, err
	}
	for _, row := range rows {
		if processed[row.ID] {
			continue
		}
		opened, openErr := opener.Open(row.Path)
		receipt := queue.Receipt{
			Kind:       queue.ReceiptKind,
			ID:         "receipt_" + row.ID,
			QueueID:    row.ID,
			Status:     "launched",
			Executable: opened.Executable,
			Args:       opened.Args,
			PID:        opened.PID,
			RecordedAt: time.Now().UTC().Format(time.RFC3339Nano),
		}
		if openErr != nil {
			receipt.Status = "failed"
			receipt.Error = openErr.Error()
		}
		if err := queue.Append(p.ReceiptPath, receipt); err != nil {
			return Result{}, err
		}
		if openErr != nil {
			return Result{Processed: 1, Receipt: &receipt}, fmt.Errorf("host.open failed: %w", openErr)
		}
		return Result{Processed: 1, Receipt: &receipt}, nil
	}
	return Result{}, nil
}
