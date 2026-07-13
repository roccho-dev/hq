package current

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"hq/internal/core"
)

// LoadRuntimeWorldJSONL preserves identity-free legacy fixtures while selecting
// the strict hq.world.v1 path whenever the explicit manifest is present.
func LoadRuntimeWorldJSONL(r io.Reader) (*core.JsonlWorld, error) {
	data, err := io.ReadAll(io.LimitReader(r, (16<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 16<<20 {
		return nil, fmt.Errorf("world exceeds 16 MiB")
	}
	selected, err := containsSelectedWorldManifest(data)
	if err != nil {
		return nil, err
	}
	if selected {
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

func containsSelectedWorldManifest(data []byte) (bool, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var discriminator struct {
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal([]byte(line), &discriminator); err != nil {
			return false, fmt.Errorf("world line %d: %w", lineNo, err)
		}
		if discriminator.Kind == core.WorldDefinitionKind {
			return true, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return false, err
	}
	return false, nil
}
