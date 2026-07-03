package hq

import (
	"io"

	"hq-reflective-poc/internal/adapter/current"
	"hq-reflective-poc/internal/core"
)

// SchemaKey is the canonical core protocol key type.
type SchemaKey = core.SchemaKey

// JsonlWorld is the canonical core protocol world type.
type JsonlWorld = core.JsonlWorld

// DefaultSchemaJSONL remains exported for compatibility during the boundary split.
// The concrete JSONL data is owned by the current adapter, not core.
const DefaultSchemaJSONL = current.DefaultSchemaJSONL

// LoadSchemaJSONL parses the current proof-era schema adapter format.
func LoadSchemaJSONL(r io.Reader) (*JsonlWorld, error) {
	return current.LoadSchemaJSONL(r)
}

func DefaultWorld() *JsonlWorld {
	return current.DefaultWorld()
}
