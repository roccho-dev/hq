package current

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
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
	seenTools := map[string]bool{}
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
			if err := decodeStrict([]byte(line), &command); err != nil {
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
		if discriminator.Kind == "hq.local-tool.v1" {
			var tool core.LocalToolDefinition
			if err := decodeStrict([]byte(line), &tool); err != nil {
				return nil, fmt.Errorf("schema line %d: %w", lineNo, err)
			}
			if err := validateLocalTool(tool); err != nil {
				return nil, fmt.Errorf("schema line %d: %w", lineNo, err)
			}
			identity := localToolIdentity(tool.ToolID, tool.ToolVersion)
			if seenTools[identity] {
				return nil, fmt.Errorf("schema line %d: duplicate local tool %q version %q", lineNo, tool.ToolID, tool.ToolVersion)
			}
			seenTools[identity] = true
			world.LocalTools = append(world.LocalTools, tool)
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
	if len(world.Keys) == 0 && len(world.Commands) == 0 && len(world.LocalTools) == 0 {
		return nil, fmt.Errorf("schema has no keys, commands, or local tools")
	}
	sortWorld(world)
	if err := validateLocalToolCommands(world); err != nil {
		return nil, err
	}
	return world, nil
}

func decodeStrict(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("record contains multiple JSON values")
		}
		return err
	}
	return nil
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

const (
	maxLocalToolStdinBytes  = 1 << 20
	maxLocalToolOutputBytes = 16 << 20
	maxLocalToolTimeoutMS   = 5 * 60 * 1000
)

func validateLocalTool(tool core.LocalToolDefinition) error {
	if tool.Kind != "hq.local-tool.v1" {
		return errors.New("local tool kind must be hq.local-tool.v1")
	}
	for field, value := range map[string]string{
		"tool_id":                  tool.ToolID,
		"tool_version":             tool.ToolVersion,
		"binding_ref":              tool.BindingRef,
		"binding_contract_version": tool.BindingContractVersion,
	} {
		if !validLocalToolName(value) {
			return fmt.Errorf("%s is invalid", field)
		}
	}
	if len(tool.Actions) == 0 {
		return errors.New("local tool requires at least one action")
	}
	seenActions := map[string]bool{}
	for i, action := range tool.Actions {
		if err := validateLocalToolAction(action); err != nil {
			return fmt.Errorf("action %d: %w", i+1, err)
		}
		if seenActions[action.ActionID] {
			return fmt.Errorf("duplicate action %q", action.ActionID)
		}
		seenActions[action.ActionID] = true
	}
	return nil
}

func validateLocalToolAction(action core.LocalToolAction) error {
	if !validLocalToolName(action.ActionID) {
		return errors.New("action_id is invalid")
	}
	seenInputs := map[string]core.LocalToolInput{}
	for i, input := range action.Inputs {
		if !validLocalToolName(input.Name) {
			return fmt.Errorf("input %d name is invalid", i+1)
		}
		if _, duplicate := seenInputs[input.Name]; duplicate {
			return fmt.Errorf("duplicate input %q", input.Name)
		}
		switch input.Type {
		case "string", "integer", "boolean":
			if len(input.Enum) != 0 {
				return fmt.Errorf("input %q type %q must not declare enum values", input.Name, input.Type)
			}
		case "enum":
			if len(input.Enum) == 0 {
				return fmt.Errorf("enum input %q requires values", input.Name)
			}
			seenValues := map[string]bool{}
			for _, value := range input.Enum {
				if strings.TrimSpace(value) == "" {
					return fmt.Errorf("enum input %q contains an empty value", input.Name)
				}
				if seenValues[value] {
					return fmt.Errorf("enum input %q contains duplicate value %q", input.Name, value)
				}
				seenValues[value] = true
			}
		default:
			return fmt.Errorf("input %q has unsupported type %q", input.Name, input.Type)
		}
		seenInputs[input.Name] = input
	}
	if len(action.Argv) == 0 {
		return errors.New("argv requires at least one template argument")
	}
	for i, argument := range action.Argv {
		hasLiteral := argument.Literal != nil
		hasField := argument.Field != nil
		if hasLiteral == hasField {
			return fmt.Errorf("argv %d must declare exactly one of literal or field", i+1)
		}
		if hasLiteral && *argument.Literal == "" {
			return fmt.Errorf("argv %d literal must not be empty", i+1)
		}
		if hasField {
			input, ok := seenInputs[*argument.Field]
			if !ok {
				return fmt.Errorf("argv %d references unknown field %q", i+1, *argument.Field)
			}
			if !input.Required {
				return fmt.Errorf("argv %d field %q must be required", i+1, *argument.Field)
			}
		}
	}
	if err := validateLocalToolStdin(action.Stdin, seenInputs); err != nil {
		return err
	}
	if action.Limits.TimeoutMS <= 0 || action.Limits.TimeoutMS > maxLocalToolTimeoutMS {
		return fmt.Errorf("limits.timeout_ms must be between 1 and %d", maxLocalToolTimeoutMS)
	}
	if action.Limits.StdoutBytes <= 0 || action.Limits.StdoutBytes > maxLocalToolOutputBytes {
		return fmt.Errorf("limits.stdout_bytes must be between 1 and %d", maxLocalToolOutputBytes)
	}
	if action.Limits.StderrBytes <= 0 || action.Limits.StderrBytes > maxLocalToolOutputBytes {
		return fmt.Errorf("limits.stderr_bytes must be between 1 and %d", maxLocalToolOutputBytes)
	}
	switch action.Output.Format {
	case "text", "json", "jsonl":
	default:
		return fmt.Errorf("unsupported output format %q", action.Output.Format)
	}
	if action.NativeRefs.Session != nil {
		if action.Output.Format == "text" {
			return errors.New("native session selector requires json or jsonl output")
		}
		if err := validateNativeSelector(*action.NativeRefs.Session); err != nil {
			return fmt.Errorf("native_refs.session: %w", err)
		}
	}
	switch action.Lifecycle {
	case "one-shot":
	default:
		return fmt.Errorf("unsupported lifecycle %q; first-child execution is one-shot only", action.Lifecycle)
	}
	switch action.Risk {
	case "low", "medium", "high":
	default:
		return fmt.Errorf("unsupported risk %q", action.Risk)
	}
	switch action.Approval {
	case "explicit":
	default:
		return fmt.Errorf("unsupported approval %q; canonical explicit approval is required", action.Approval)
	}
	return nil
}

func validateLocalToolStdin(stdin core.LocalToolStdin, inputs map[string]core.LocalToolInput) error {
	switch stdin.Mode {
	case "none":
		if stdin.Field != "" || stdin.MaxBytes != 0 {
			return errors.New("stdin mode none must not declare field or max_bytes")
		}
	case "field":
		input, ok := inputs[stdin.Field]
		if !ok {
			return fmt.Errorf("stdin references unknown field %q", stdin.Field)
		}
		if input.Type != "string" {
			return errors.New("stdin field must have string type")
		}
		if !input.Required {
			return errors.New("stdin field must be required")
		}
		if stdin.MaxBytes <= 0 || stdin.MaxBytes > maxLocalToolStdinBytes {
			return fmt.Errorf("stdin.max_bytes must be between 1 and %d", maxLocalToolStdinBytes)
		}
	default:
		return fmt.Errorf("unsupported stdin mode %q", stdin.Mode)
	}
	return nil
}

func validateNativeSelector(selector core.LocalToolNativeSelector) error {
	if selector.Source != "stdout" {
		return errors.New("source must be stdout")
	}
	if len(selector.Path) == 0 {
		return errors.New("path requires at least one field segment")
	}
	for _, segment := range selector.Path {
		if !validLocalToolName(segment) {
			return fmt.Errorf("invalid path segment %q", segment)
		}
	}
	return nil
}

func validLocalToolName(value string) bool {
	if value == "" || strings.TrimSpace(value) != value {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func localToolIdentity(toolID, version string) string {
	return toolID + "\x00" + version
}

func sortWorld(world *core.JsonlWorld) {
	sort.Slice(world.Commands, func(i, j int) bool { return world.Commands[i].Name < world.Commands[j].Name })
	for i := range world.LocalTools {
		sort.Slice(world.LocalTools[i].Actions, func(a, b int) bool {
			return world.LocalTools[i].Actions[a].ActionID < world.LocalTools[i].Actions[b].ActionID
		})
		for a := range world.LocalTools[i].Actions {
			sort.Slice(world.LocalTools[i].Actions[a].Inputs, func(x, y int) bool {
				return world.LocalTools[i].Actions[a].Inputs[x].Name < world.LocalTools[i].Actions[a].Inputs[y].Name
			})
		}
	}
	sort.Slice(world.LocalTools, func(i, j int) bool {
		if world.LocalTools[i].ToolID == world.LocalTools[j].ToolID {
			return world.LocalTools[i].ToolVersion < world.LocalTools[j].ToolVersion
		}
		return world.LocalTools[i].ToolID < world.LocalTools[j].ToolID
	})
}

func validateLocalToolCommands(world *core.JsonlWorld) error {
	for _, command := range world.Commands {
		target, _ := command.Instruction["target"].(string)
		if target != "local-tool" {
			continue
		}
		if command.Instruction["version"] != "instruction.v1" || command.Instruction["op"] != "run" {
			return fmt.Errorf("command %q local-tool instruction requires instruction.v1 run", command.Name)
		}
		instructionFields := map[string]bool{"version": true, "op": true, "target": true, "payload": true, "reason": true, "reply_to": true, "labels": true, "policy": true}
		for key := range command.Instruction {
			if !instructionFields[key] {
				return fmt.Errorf("command %q local-tool instruction contains unsupported field %q", command.Name, key)
			}
		}
		payload, ok := command.Instruction["payload"].(map[string]any)
		if !ok {
			return fmt.Errorf("command %q local-tool payload must be an object", command.Name)
		}
		allowed := map[string]bool{"tool_id": true, "tool_version": true, "action_id": true, "input": true}
		for key := range payload {
			if !allowed[key] {
				return fmt.Errorf("command %q local-tool payload contains unsupported field %q", command.Name, key)
			}
		}
		toolID, toolOK := payload["tool_id"].(string)
		version, versionOK := payload["tool_version"].(string)
		actionID, actionOK := payload["action_id"].(string)
		input, inputOK := payload["input"].(map[string]any)
		if !toolOK || !versionOK || !actionOK || !inputOK {
			return fmt.Errorf("command %q local-tool payload requires string tool_id/tool_version/action_id and object input", command.Name)
		}
		tool, ok := world.LocalTool(toolID, version)
		if !ok {
			return fmt.Errorf("command %q references unknown local tool %q version %q", command.Name, toolID, version)
		}
		action, ok := localToolAction(tool, actionID)
		if !ok {
			return fmt.Errorf("command %q references unknown action %q", command.Name, actionID)
		}
		inputs := map[string]core.LocalToolInput{}
		for _, definition := range action.Inputs {
			inputs[definition.Name] = definition
		}
		provided := map[string]bool{}
		for name, value := range input {
			definition, exists := inputs[name]
			if !exists {
				return fmt.Errorf("command %q provides unknown action input %q", command.Name, name)
			}
			if !localToolValueMatches(definition, value) {
				return fmt.Errorf("command %q provides type-invalid action input %q", command.Name, name)
			}
			provided[name] = true
		}
		for _, field := range command.Fields {
			const prefix = "payload.input."
			if !strings.HasPrefix(field.Bind, prefix) || strings.TrimPrefix(field.Bind, prefix) == "" || strings.Contains(strings.TrimPrefix(field.Bind, prefix), ".") {
				return fmt.Errorf("command %q field %q must bind directly under payload.input", command.Name, field.Name)
			}
			name := strings.TrimPrefix(field.Bind, prefix)
			definition, exists := inputs[name]
			if !exists {
				return fmt.Errorf("command %q field %q binds unknown action input %q", command.Name, field.Name, name)
			}
			if provided[name] {
				return fmt.Errorf("command %q action input %q is provided more than once", command.Name, name)
			}
			if !localToolFieldMatches(field, definition) {
				return fmt.Errorf("command %q field %q does not match action input %q", command.Name, field.Name, name)
			}
			provided[name] = true
		}
		for _, definition := range action.Inputs {
			if definition.Required && !provided[definition.Name] {
				return fmt.Errorf("command %q does not provide required action input %q", command.Name, definition.Name)
			}
		}
	}
	return nil
}

func localToolAction(tool core.LocalToolDefinition, actionID string) (core.LocalToolAction, bool) {
	for _, action := range tool.Actions {
		if action.ActionID == actionID {
			return action, true
		}
	}
	return core.LocalToolAction{}, false
}

func localToolFieldMatches(field core.CommandField, input core.LocalToolInput) bool {
	fieldType := field.Type
	if fieldType == "path" {
		fieldType = "string"
	}
	if fieldType != input.Type || field.Required != input.Required {
		return false
	}
	if input.Type != "enum" || len(field.Enum) != len(input.Enum) {
		return input.Type != "enum"
	}
	for i := range input.Enum {
		if field.Enum[i] != input.Enum[i] {
			return false
		}
	}
	return true
}

func localToolValueMatches(input core.LocalToolInput, value any) bool {
	switch input.Type {
	case "string":
		_, ok := value.(string)
		return ok
	case "integer":
		value, ok := value.(float64)
		return ok && value == float64(int64(value))
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "enum":
		value, ok := value.(string)
		if !ok {
			return false
		}
		for _, allowed := range input.Enum {
			if value == allowed {
				return true
			}
		}
	}
	return false
}

func DefaultWorld() *core.JsonlWorld {
	w, err := LoadSchemaJSONL(bytes.NewBufferString(DefaultSchemaJSONL))
	if err != nil {
		panic(err)
	}
	return w
}
