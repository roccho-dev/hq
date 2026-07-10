package hq

import (
	"io"

	"hq/internal/adapter/current"
	"hq/internal/core"
)

// SchemaKey is the canonical core protocol key type.
type SchemaKey = core.SchemaKey
type CommandField = core.CommandField
type CommandDefinition = core.CommandDefinition

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
