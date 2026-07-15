package hq

import (
	"encoding/json"
	"io"
)

type QueueWriter struct {
	W io.Writer
}

func (q QueueWriter) Append(d CompileDraft) error {
	if d.AcceptedInput != nil {
		if err := d.AcceptedInput.Validate(); err != nil {
			return err
		}
	}
	enc := json.NewEncoder(q.W)
	enc.SetEscapeHTML(false)
	return enc.Encode(d)
}
