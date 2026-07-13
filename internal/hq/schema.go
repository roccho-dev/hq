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
type WorldDefinition = core.WorldDefinition
type WorldRef = core.WorldRef
type CommandRef = core.CommandRef
type CompileProvenance = core.CompileProvenance

const (
	WorldDefinitionKind    = core.WorldDefinitionKind
	CompileProvenanceKind  = core.CompileProvenanceKind
	CommandInputKind       = core.CommandInputKind
	CanonicalJSONInputKind = core.CanonicalJSONInputKind
)

// JsonlWorld is the canonical core protocol world type.
type JsonlWorld = core.JsonlWorld

// DefaultSchemaJSONL remains exported for compatibility during the boundary split.
// The concrete JSONL data is owned by the current adapter, not core.
const DefaultSchemaJSONL = current.DefaultSchemaJSONL

// LoadSchemaJSONL parses the current proof-era schema adapter format. It remains
// the compatibility loader for identity-free fixtures and legacy inputs.
func LoadSchemaJSONL(r io.Reader) (*JsonlWorld, error) {
	return current.LoadSchemaJSONL(r)
}

// LoadSelectedWorldJSONL requires one explicit immutable selected-world identity
// and stable identity/version on every command definition.
func LoadSelectedWorldJSONL(r io.Reader) (*JsonlWorld, error) {
	return current.LoadSelectedWorldJSONL(r)
}

func DefaultWorld() *JsonlWorld {
	return current.DefaultWorld()
}
