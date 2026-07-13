package current

import (
	"bytes"
	"fmt"
	"io"

	"hq/internal/core"
)

// LoadRuntimeWorldJSONL preserves identity-free legacy fixtures while selecting
// the strict hq.world.v1 path whenever the explicit manifest is present.
func LoadRuntimeWorldJSONL(r io.Reader) (*core.JsonlWorld, error) {
	data, err := io.ReadAll(io.LimitReader(r, 16<<20))
	if err != nil {
		return nil, err
	}
	if len(data) == 16<<20 {
		return nil, fmt.Errorf("world exceeds 16 MiB")
	}
	if bytes.Contains(data, []byte(`"kind":"hq.world.v1"`)) || bytes.Contains(data, []byte(`"kind": "hq.world.v1"`)) {
		return LoadSelectedWorldJSONL(bytes.NewReader(data))
	}
	world, err := LoadSchemaJSONL(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	// Legacy worlds receive a semantic digest for diagnostics but are not a
	// selected-world identity and cannot produce provenance-backed claims.
	normalizeSelectedWorld(world)
	if err := world.RecomputeDigest(); err != nil {
		return nil, err
	}
	return world, nil
}
