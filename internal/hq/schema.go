package hq

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

// SchemaKey describes one JSONL key that the line editor may propose.
type SchemaKey struct {
	Key         string   `json:"key"`
	Type        string   `json:"type,omitempty"`
	Required    bool     `json:"required,omitempty"`
	Description string   `json:"description,omitempty"`
	Group       string   `json:"group,omitempty"`
	Enum        []string `json:"enum,omitempty"`
	Examples    []string `json:"examples,omitempty"`
}

// JsonlWorld is the small, reusable domain model the UI projects into a line editor.
type JsonlWorld struct {
	Keys []SchemaKey      `json:"keys"`
	Rows []map[string]any `json:"rows,omitempty"`
}

// DefaultSchemaJSONL is intentionally data, not Go logic. Replacing this JSONL changes the suggestions.
const DefaultSchemaJSONL = `{"key":"op","type":"enum","required":true,"description":"operation to compile into instruction","group":"instruction","enum":["queue.create","queue.preview","queue.dispatch"]}
{"key":"target","type":"enum","required":true,"description":"resolver / dispatch target","group":"instruction","enum":["ctx","local","windows","nixos","ssh"]}
{"key":"priority","type":"enum","required":false,"description":"queue ranking hint","group":"instruction","enum":["high","normal","low"]}
{"key":"payload","type":"object","required":true,"description":"user data or operation arguments","group":"data"}
{"key":"reason","type":"string","required":false,"description":"human audit trail for why this instruction exists","group":"audit"}
{"key":"path","type":"string","required":false,"description":"file path used by payload or target","group":"data"}
{"key":"host","type":"string","required":false,"description":"remote host or machine name","group":"dispatch"}
`

// LoadSchemaJSONL parses one SchemaKey per JSON line.
func LoadSchemaJSONL(r io.Reader) (*JsonlWorld, error) {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	world := &JsonlWorld{}
	lineNo := 0
	seen := map[string]bool{}
	for s.Scan() {
		lineNo++
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var k SchemaKey
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

func DefaultWorld() *JsonlWorld {
	w, err := LoadSchemaJSONL(bytes.NewBufferString(DefaultSchemaJSONL))
	if err != nil {
		panic(err)
	}
	return w
}

func (w *JsonlWorld) Key(name string) (SchemaKey, bool) {
	if w == nil {
		return SchemaKey{}, false
	}
	for _, k := range w.Keys {
		if k.Key == name {
			return k, true
		}
	}
	return SchemaKey{}, false
}

func (w *JsonlWorld) RequiredKeys() []string {
	if w == nil {
		return nil
	}
	out := make([]string, 0)
	for _, k := range w.Keys {
		if k.Required {
			out = append(out, k.Key)
		}
	}
	sort.Strings(out)
	return out
}
