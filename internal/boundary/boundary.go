package boundary

import "hq-reflective-poc/internal/core"

type WorldReader interface {
	ReadWorld() (*core.JsonlWorld, error)
}

type DraftAppender interface {
	Append(core.CompileDraft) error
}
