package hq

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type CommandDiagnostic struct {
	Line    int
	Start   int
	End     int
	Code    string
	Message string
}

type documentLine struct {
	Text  string
	Start int
}

type commandObject struct {
	Name       string
	StartLine  int
	EndLine    int
	Values     map[string]string
	FieldLines map[string]int
}

func commandSuggestions(buffer string, cursor int, world *JsonlWorld) []Suggestion {
	lines := documentLines(buffer)
	lineIndex, localCursor := lineAtOffset(lines, cursor)
	line := lines[lineIndex]
	before := line.Text[:min(localCursor, len(line.Text))]
	trimmed := strings.TrimSpace(before)

	if strings.HasPrefix(trimmed, "@") || !hasCommandHeaderAtOrBefore(lines, lineIndex) {
		partial := strings.TrimSpace(strings.TrimPrefix(trimmed, "@"))
		out := []Suggestion{}
		for _, command := range world.Commands {
			if partial != "" && !match(command.Name, partial) {
				continue
			}
			insert := "@" + command.Name
			out = append(out, Suggestion{
				Label: command.Name, InsertText: insert, Detail: "hq command object",
				Description: command.Description, Tag: "command", Score: 100,
				Edit:  TextEdit{Start: line.Start, End: line.Start + len(line.Text), Text: insert},
				Draft: CompileDraft{Kind: "candidate.command", Queue: "instruction.jsonl", Reason: "command declared by profile world"},
			})
		}
		sortSuggestions(out)
		return out
	}

	object, err := parseCommandObjectIgnoring(lines, lineIndex, lineIndex)
	if err != nil {
		return nil
	}
	definition, ok := world.Command(object.Name)
	if !ok || lineIndex == object.StartLine {
		return nil
	}

	active := strings.TrimSpace(before)
	if key, partial, hasValue := strings.Cut(active, "="); hasValue {
		field, ok := commandField(definition, strings.TrimSpace(key))
		if !ok {
			return nil
		}
		values := field.Enum
		if len(values) == 0 {
			values = field.Examples
		}
		out := []Suggestion{}
		valueStart := line.Start + strings.Index(line.Text, "=") + 1
		clean := strings.Trim(strings.TrimSpace(partial), "\"")
		for _, value := range values {
			if clean != "" && !match(value, clean) {
				continue
			}
			insert := quoteCommandValue(value)
			out = append(out, Suggestion{
				Label: value, InsertText: insert, Detail: field.Type,
				Description: field.Description, Tag: "command value", Score: 80,
				Edit:  TextEdit{Start: valueStart, End: line.Start + len(line.Text), Text: insert},
				Draft: CompileDraft{Kind: "candidate.commandValue", Queue: "instruction.jsonl", Key: field.Name, Value: value, Reason: "value declared by profile world"},
			})
		}
		sortSuggestions(out)
		return out
	}

	partial := strings.TrimSpace(active)
	out := []Suggestion{}
	for _, field := range definition.Fields {
		if existingLine, used := object.FieldLines[field.Name]; used && existingLine != lineIndex {
			continue
		}
		if partial != "" && !match(field.Name, partial) {
			continue
		}
		insert := field.Name + "="
		out = append(out, Suggestion{
			Label: field.Name, InsertText: insert, Detail: field.Type,
			Description: field.Description, Tag: "command field", Score: fieldScore(field),
			Edit:  TextEdit{Start: line.Start, End: line.Start + len(line.Text), Text: insert},
			Draft: CompileDraft{Kind: "candidate.commandField", Queue: "instruction.jsonl", Key: field.Name, Reason: "field declared by profile world"},
		})
	}
	sortSuggestions(out)
	return out
}

// CompileCommandObject lowers the @command object containing cursorLine. The
// editor layout is not durable meaning; exactly one accepted instruction is.
func CompileCommandObject(text string, cursorLine int, world *JsonlWorld) (CompileDraft, error) {
	if world == nil || len(world.Commands) == 0 {
		return CompileDraft{}, errors.New("profile world has no command definitions")
	}
	lines := documentLines(text)
	if cursorLine < 0 || cursorLine >= len(lines) {
		return CompileDraft{}, errors.New("submit line is outside document")
	}
	object, err := parseCommandObject(lines, cursorLine)
	if err != nil {
		return CompileDraft{}, err
	}
	definition, ok := world.Command(object.Name)
	if !ok {
		return CompileDraft{}, fmt.Errorf("unknown command %q", object.Name)
	}
	for key := range object.Values {
		if _, ok := commandField(definition, key); !ok {
			return CompileDraft{}, fmt.Errorf("unknown field %q for %s", key, definition.Name)
		}
	}
	instruction := cloneMap(definition.Instruction)
	for _, field := range definition.Fields {
		raw, present := object.Values[field.Name]
		if !present {
			if field.Required {
				return CompileDraft{}, fmt.Errorf("required field %q is missing", field.Name)
			}
			continue
		}
		value, err := commandValue(field, raw)
		if err != nil {
			return CompileDraft{}, err
		}
		if err := bindValue(instruction, field.Bind, value); err != nil {
			return CompileDraft{}, fmt.Errorf("field %q: %w", field.Name, err)
		}
	}
	return CompileDraft{Kind: "accepted.instruction", Queue: "instruction.jsonl", Instruction: instruction, Reason: "command object accepted by human"}, nil
}

// CompileCommandLine remains a narrow compatibility entrypoint for callers
// that already hold exactly one object. New editor code should pass the whole
// document and cursor line to CompileCommandObject.
func CompileCommandLine(text string, world *JsonlWorld) (CompileDraft, error) {
	return CompileCommandObject(text, 0, world)
}

func ValidateCommandDocument(text string, world *JsonlWorld) []CommandDiagnostic {
	if world == nil || len(world.Commands) == 0 {
		return nil
	}
	lines := documentLines(text)
	out := []CommandDiagnostic{}
	for index := 0; index < len(lines); {
		trimmed := strings.TrimSpace(lines[index].Text)
		if trimmed == "" {
			index++
			continue
		}
		if strings.HasPrefix(trimmed, "{") {
			if !json.Valid([]byte(trimmed)) {
				out = append(out, CommandDiagnostic{Line: index, End: len([]rune(lines[index].Text)), Code: "invalid-json", Message: "line is not one complete JSON object"})
			}
			index++
			continue
		}
		if !strings.HasPrefix(trimmed, "@") {
			out = append(out, CommandDiagnostic{Line: index, End: len([]rune(lines[index].Text)), Code: "missing-command-header", Message: "field is outside an @command object"})
			index++
			continue
		}
		end := nextCommandHeader(lines, index+1)
		if _, err := CompileCommandObject(text, index, world); err != nil {
			out = append(out, CommandDiagnostic{Line: index, End: len([]rune(lines[index].Text)), Code: "invalid-command-object", Message: err.Error()})
		}
		index = end
	}
	return out
}

func LineAt(text string, line int) (string, error) {
	lines := documentLines(text)
	if line < 0 || line >= len(lines) {
		return "", errors.New("submit line is outside document")
	}
	if strings.TrimSpace(lines[line].Text) == "" {
		return "", errors.New("submit line is empty")
	}
	return lines[line].Text, nil
}

func documentLines(text string) []documentLine {
	raw := strings.Split(text, "\n")
	lines := make([]documentLine, len(raw))
	offset := 0
	for index, line := range raw {
		line = strings.TrimSuffix(line, "\r")
		lines[index] = documentLine{Text: line, Start: offset}
		offset += len(raw[index]) + 1
	}
	return lines
}

func currentLine(buffer string, cursor int) (string, int) {
	lines := documentLines(buffer)
	index, _ := lineAtOffset(lines, cursor)
	return lines[index].Text, lines[index].Start
}

func lineAtOffset(lines []documentLine, offset int) (int, int) {
	if offset < 0 {
		offset = 0
	}
	for index := len(lines) - 1; index >= 0; index-- {
		if offset >= lines[index].Start {
			local := offset - lines[index].Start
			if local > len(lines[index].Text) {
				local = len(lines[index].Text)
			}
			return index, local
		}
	}
	return 0, 0
}

func hasCommandHeaderAtOrBefore(lines []documentLine, line int) bool {
	for index := line; index >= 0; index-- {
		if strings.HasPrefix(strings.TrimSpace(lines[index].Text), "@") {
			return true
		}
	}
	return false
}

func parseCommandObject(lines []documentLine, cursorLine int) (commandObject, error) {
	return parseCommandObjectIgnoring(lines, cursorLine, -1)
}

func parseCommandObjectIgnoring(lines []documentLine, cursorLine, ignoredLine int) (commandObject, error) {
	start := -1
	for index := cursorLine; index >= 0; index-- {
		if strings.HasPrefix(strings.TrimSpace(lines[index].Text), "@") {
			start = index
			break
		}
	}
	if start < 0 {
		return commandObject{}, errors.New("submit position is outside an @command object")
	}
	header := strings.TrimSpace(lines[start].Text)
	name := strings.TrimSpace(strings.TrimPrefix(header, "@"))
	if name == "" || strings.ContainsAny(name, " \t=\"") {
		return commandObject{}, errors.New("command header must be exactly @name")
	}
	end := nextCommandHeader(lines, start+1)
	if cursorLine >= end {
		return commandObject{}, errors.New("submit position is outside an @command object")
	}
	object := commandObject{Name: name, StartLine: start, EndLine: end, Values: map[string]string{}, FieldLines: map[string]int{}}
	for index := start + 1; index < end; index++ {
		if index == ignoredLine {
			continue
		}
		line := strings.TrimSpace(lines[index].Text)
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if !ok || key == "" || value == "" {
			return commandObject{}, fmt.Errorf("line %d must be key=value", index+1)
		}
		if _, duplicate := object.Values[key]; duplicate {
			return commandObject{}, fmt.Errorf("field %q is duplicated", key)
		}
		unquoted, err := unquoteCommandValue(value)
		if err != nil {
			return commandObject{}, fmt.Errorf("field %q: %w", key, err)
		}
		object.Values[key] = unquoted
		object.FieldLines[key] = index
	}
	return object, nil
}

func nextCommandHeader(lines []documentLine, from int) int {
	for index := from; index < len(lines); index++ {
		if strings.HasPrefix(strings.TrimSpace(lines[index].Text), "@") {
			return index
		}
	}
	return len(lines)
}

func unquoteCommandValue(value string) (string, error) {
	if !strings.HasPrefix(value, "\"") {
		if strings.ContainsAny(value, " \t") {
			return "", errors.New("values containing whitespace must be quoted")
		}
		return value, nil
	}
	if len(value) < 2 || !strings.HasSuffix(value, "\"") {
		return "", errors.New("unterminated quoted value")
	}
	return value[1 : len(value)-1], nil
}

func commandField(command CommandDefinition, name string) (CommandField, bool) {
	for _, field := range command.Fields {
		if field.Name == name {
			return field, true
		}
	}
	return CommandField{}, false
}

func commandValue(field CommandField, raw string) (any, error) {
	switch field.Type {
	case "string", "path":
		return raw, nil
	case "enum":
		for _, candidate := range field.Enum {
			if raw == candidate {
				return raw, nil
			}
		}
		return nil, fmt.Errorf("field %q must be one of %s", field.Name, strings.Join(field.Enum, ", "))
	case "integer":
		value, err := strconv.Atoi(raw)
		if err != nil {
			return nil, fmt.Errorf("field %q must be an integer", field.Name)
		}
		return value, nil
	case "boolean":
		value, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("field %q must be true or false", field.Name)
		}
		return value, nil
	default:
		return nil, fmt.Errorf("field %q has unsupported type %q", field.Name, field.Type)
	}
}

func bindValue(root map[string]any, path string, value any) error {
	parts := strings.Split(path, ".")
	if len(parts) == 0 {
		return errors.New("bind path is empty")
	}
	current := root
	for _, part := range parts[:len(parts)-1] {
		if part == "" {
			return errors.New("bind path contains an empty segment")
		}
		next, ok := current[part]
		if !ok {
			child := map[string]any{}
			current[part] = child
			current = child
			continue
		}
		child, ok := next.(map[string]any)
		if !ok {
			return fmt.Errorf("bind path %q crosses a non-object", path)
		}
		current = child
	}
	leaf := parts[len(parts)-1]
	if leaf == "" {
		return errors.New("bind path has an empty leaf")
	}
	current[leaf] = value
	return nil
}

func cloneMap(input map[string]any) map[string]any {
	encoded, _ := json.Marshal(input)
	var output map[string]any
	_ = json.Unmarshal(encoded, &output)
	return output
}

func quoteCommandValue(value string) string {
	if strings.ContainsAny(value, " \t") {
		return `"` + value + `"`
	}
	return value
}

func fieldScore(field CommandField) int {
	if field.Required {
		return 100
	}
	return 50
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
