package boundary

import "hq/internal/core"

type WorldReader interface {
	ReadWorld() (*core.JsonlWorld, error)
}

type DraftAppender interface {
	Append(core.CompileDraft) error
}
