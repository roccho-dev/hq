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

// DefaultSchemaJSONL is current proof-era adapter data. It is not core logic.
const DefaultSchemaJSONL = `{"key":"op","type":"enum","required":true,"description":"operation to compile into instruction","group":"instruction","enum":["queue.create","queue.preview","queue.dispatch"]}
{"key":"target","type":"enum","required":true,"description":"resolver / dispatch target","group":"instruction","enum":["ctx","local","windows","nixos","ssh"]}
{"key":"priority","type":"enum","required":false,"description":"queue ranking hint","group":"instruction","enum":["high","normal","low"]}
{"key":"payload","type":"object","required":true,"description":"user data or operation arguments","group":"data"}
{"key":"reason","type":"string","required":false,"description":"human audit trail for why this instruction exists","group":"audit"}
{"key":"path","type":"string","required":false,"description":"file path used by payload or target","group":"data"}
{"key":"host","type":"string","required":false,"description":"remote host or machine name","group":"dispatch"}
`

func LoadSchemaJSONL(r io.Reader) (*core.JsonlWorld, error) {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	world := &core.JsonlWorld{}
	seen := map[string]bool{}
	lineNo := 0
	for s.Scan() {
		lineNo++
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var k core.SchemaKey
		if err := json.Unmarshal([]byte(line), &k); err != nil {
			return nil, fmt.Errorf("schema line %d: %w", lineNo, err)
		}
		k.Key = strings.TrimSpace(k.Key)
		if k.Key == "" {
			return nil, fmt.Errorf("schema line %d: key is required", lineNo)
		}
		if seen[k.Key] {
			return nil, fmt.Errorf("schema line %d: duplicate key %q", lineNo, k.Key)
		}
		seen[k.Key] = true
		world.Keys = append(world.Keys, k)
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	if len(world.Keys) == 0 {
		return nil, fmt.Errorf("schema has no keys")
	}
	return world, nil
}

func DefaultWorld() *core.JsonlWorld {
	w, err := LoadSchemaJSONL(bytes.NewBufferString(DefaultSchemaJSONL))
	if err != nil {
		panic(err)
	}
	return w
}
