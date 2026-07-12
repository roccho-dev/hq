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
	seenCommands := map[string]bool{}
	lineNo := 0
	for s.Scan() {
		lineNo++
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var discriminator struct {
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal([]byte(line), &discriminator); err != nil {
			return nil, fmt.Errorf("schema line %d: %w", lineNo, err)
		}
		if discriminator.Kind == "hq.command.v1" {
			var command core.CommandDefinition
			if err := json.Unmarshal([]byte(line), &command); err != nil {
				return nil, fmt.Errorf("schema line %d: %w", lineNo, err)
			}
			if err := validateCommand(command); err != nil {
				return nil, fmt.Errorf("schema line %d: %w", lineNo, err)
			}
			if seenCommands[command.Name] {
				return nil, fmt.Errorf("schema line %d: duplicate command %q", lineNo, command.Name)
			}
			seenCommands[command.Name] = true
			world.Commands = append(world.Commands, command)
			continue
		}
		if discriminator.Kind != "" {
			return nil, fmt.Errorf("schema line %d: unsupported kind %q", lineNo, discriminator.Kind)
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
	if len(world.Keys) == 0 && len(world.Commands) == 0 {
		return nil, fmt.Errorf("schema has no keys or commands")
	}
	return world, nil
}

func validateCommand(command core.CommandDefinition) error {
	command.Name = strings.TrimSpace(command.Name)
	if command.Name == "" || strings.ContainsAny(command.Name, " \t\r\n=\"") {
		return fmt.Errorf("command name is invalid")
	}
	if command.Instruction == nil {
		return fmt.Errorf("command %q requires instruction", command.Name)
	}
	for _, reserved := range []string{"id", "created_at"} {
		if _, ok := command.Instruction[reserved]; ok {
			return fmt.Errorf("command %q instruction must not declare reserved field %q", command.Name, reserved)
		}
	}
	seen := map[string]bool{}
	for _, field := range command.Fields {
		if strings.TrimSpace(field.Name) == "" || strings.ContainsAny(field.Name, " \t\r\n=\"") {
			return fmt.Errorf("command %q has invalid field name", command.Name)
		}
		if seen[field.Name] {
			return fmt.Errorf("command %q has duplicate field %q", command.Name, field.Name)
		}
		seen[field.Name] = true
		switch field.Type {
		case "string", "path", "enum", "integer", "boolean":
		default:
			return fmt.Errorf("command %q field %q has unsupported type %q", command.Name, field.Name, field.Type)
		}
		if strings.TrimSpace(field.Bind) == "" {
			return fmt.Errorf("command %q field %q requires bind", command.Name, field.Name)
		}
		root := strings.Split(field.Bind, ".")[0]
		if root == "id" || root == "created_at" {
			return fmt.Errorf("command %q field %q binds reserved identity field %q", command.Name, field.Name, root)
		}
	}
	return nil
}

func DefaultWorld() *core.JsonlWorld {
	w, err := LoadSchemaJSONL(bytes.NewBufferString(DefaultSchemaJSONL))
	if err != nil {
		panic(err)
	}
	return w
}
