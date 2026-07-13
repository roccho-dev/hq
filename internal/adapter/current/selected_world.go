package current

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"hq/internal/core"
)

// LoadSelectedWorldJSONL loads the one immutable aggregate world selected by an
// active profile. The hq.world.v1 row is identity metadata; all remaining rows
// continue through the existing strict adapter loader.
func LoadSelectedWorldJSONL(r io.Reader) (*core.JsonlWorld, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var semantic bytes.Buffer
	var identity *core.WorldDefinition
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
			return nil, fmt.Errorf("selected world line %d: %w", lineNo, err)
		}
		if discriminator.Kind == core.WorldDefinitionKind {
			if identity != nil {
				return nil, fmt.Errorf("selected world line %d: duplicate %s record", lineNo, core.WorldDefinitionKind)
			}
			var candidate core.WorldDefinition
			if err := decodeStrict([]byte(line), &candidate); err != nil {
				return nil, fmt.Errorf("selected world line %d: %w", lineNo, err)
			}
			identity = &candidate
			continue
		}
		semantic.WriteString(line)
		semantic.WriteByte('\n')
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if identity == nil {
		return nil, fmt.Errorf("selected world requires one %s record", core.WorldDefinitionKind)
	}
	world, err := LoadSchemaJSONL(bytes.NewReader(semantic.Bytes()))
	if err != nil {
		return nil, err
	}
	world.Identity = identity
	normalizeSelectedWorld(world)
	if err := world.RecomputeDigest(); err != nil {
		return nil, fmt.Errorf("digest selected world: %w", err)
	}
	if err := world.ValidateSelected(); err != nil {
		return nil, err
	}
	return world, nil
}

func normalizeSelectedWorld(world *core.JsonlWorld) {
	sort.Slice(world.Keys, func(i, j int) bool { return world.Keys[i].Key < world.Keys[j].Key })
	sort.Slice(world.Rows, func(i, j int) bool {
		left, _ := json.Marshal(world.Rows[i])
		right, _ := json.Marshal(world.Rows[j])
		return bytes.Compare(left, right) < 0
	})
}
