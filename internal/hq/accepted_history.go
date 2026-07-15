package hq

import "hq/internal/core"

// AttachAcceptedHistory composes policy-safe accepted evidence into the
// existing candidate identity and edit contract.
func AttachAcceptedHistory(world *JsonlWorld, base *core.WorldRecallIndex, records []core.AcceptedHistoryRecord) (*core.WorldRecallIndex, core.AcceptedHistoryReport) {
	return core.AttachAcceptedHistory(world, base, records, candidateID)
}
