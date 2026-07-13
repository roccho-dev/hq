package hq

import (
	"encoding/json"
	"sort"
	"strings"
)

type TextEdit struct {
	Start int    `json:"start"`
	End   int    `json:"end"`
	Text  string `json:"text"`
}

type CompileDraft struct {
	Kind        string             `json:"kind"`
	Queue       string             `json:"queue"`
	Key         string             `json:"key,omitempty"`
	Value       any                `json:"value,omitempty"`
	Instruction map[string]any     `json:"instruction"`
	Reason      string             `json:"reason,omitempty"`
	Provenance  *CompileProvenance `json:"provenance,omitempty"`
}

type Suggestion struct {
	Label       string       `json:"label"`
	InsertText  string       `json:"insertText"`
	Detail      string       `json:"detail,omitempty"`
	Description string       `json:"description,omitempty"`
	Tag         string       `json:"tag,omitempty"`
	Score       int          `json:"score"`
	Edit        TextEdit     `json:"edit"`
	Draft       CompileDraft `json:"compileDraft"`
}

func Complete(buffer string, cursor int, world *JsonlWorld) []Suggestion {
	if world == nil {
		world = DefaultWorld()
	}
	if len(world.Commands) > 0 {
		line, _ := currentLine(buffer, cursor)
		if !strings.HasPrefix(strings.TrimSpace(line), "{") {
			return commandSuggestions(buffer, cursor, world)
		}
	}
	ctx := Analyze(buffer, cursor, world)
	switch ctx.Kind {
	case CursorValue:
		return valueSuggestions(ctx, world)
	case CursorKey:
		return keySuggestions(ctx, world)
	default:
		return nil
	}
}

func keySuggestions(ctx CursorContext, world *JsonlWorld) []Suggestion {
	present := map[string]bool{}
	for _, k := range ctx.PresentKeys {
		present[k] = true
	}
	partial := cleanPartial(ctx.Partial)
	out := make([]Suggestion, 0, len(world.Keys))
	start, end := keyEditRange(ctx.Buffer, ctx.Cursor)
	for _, k := range world.Keys {
		if present[k.Key] {
			continue
		}
		if partial != "" && !match(k.Key, partial) {
			continue
		}
		insert := quote(k.Key) + ":"
		score := 10
		if k.Required {
			score += 100
		}
		if strings.HasPrefix(strings.ToLower(k.Key), strings.ToLower(partial)) {
			score += 20
		}
		out = append(out, Suggestion{
			Label:       quote(k.Key),
			InsertText:  insert,
			Detail:      detailForKey(k),
			Description: k.Description,
			Tag:         tag(k.Group, "schema keys"),
			Score:       score,
			Edit:        TextEdit{Start: start, End: end, Text: insert},
			Draft: CompileDraft{
				Kind:  "candidate.key",
				Queue: "instruction.jsonl",
				Key:   k.Key,
				Instruction: map[string]any{
					"op":       "queue.preview",
					"key":      k.Key,
					"required": k.Required,
				},
				Reason: "key accepted by cursor context",
			},
		})
	}
	sortSuggestions(out)
	return out
}

func valueSuggestions(ctx CursorContext, world *JsonlWorld) []Suggestion {
	k, ok := world.Key(ctx.ActiveKey)
	if !ok || len(k.Enum) == 0 {
		return nil
	}
	partial := cleanPartial(ctx.Partial)
	start, end := valueEditRange(ctx.Buffer, ctx.Cursor)
	out := make([]Suggestion, 0, len(k.Enum))
	for _, v := range k.Enum {
		if partial != "" && !match(v, partial) {
			continue
		}
		insert := quote(v)
		score := 10
		if strings.HasPrefix(strings.ToLower(v), strings.ToLower(partial)) {
			score += 20
		}
		if k.Required {
			score += 30
		}
		inst := map[string]any{
			"op":    "queue.preview",
			"key":   k.Key,
			"value": v,
		}
		if k.Key == "op" {
			inst["op"] = v
		} else {
			inst[k.Key] = v
		}
		out = append(out, Suggestion{
			Label:       v,
			InsertText:  insert,
			Detail:      "value for " + quote(k.Key),
			Description: k.Description,
			Tag:         tag(k.Group, "value candidates"),
			Score:       score,
			Edit:        TextEdit{Start: start, End: end, Text: insert},
			Draft: CompileDraft{
				Kind:        "candidate.value",
				Queue:       "instruction.jsonl",
				Key:         k.Key,
				Value:       v,
				Instruction: inst,
				Reason:      "enum value accepted by cursor context",
			},
		})
	}
	sortSuggestions(out)
	return out
}

func CompileLine(buffer string, world *JsonlWorld) CompileDraft {
	if world == nil {
		world = DefaultWorld()
	}
	inst := map[string]any{}
	_ = json.Unmarshal([]byte(buffer), &inst)
	if len(inst) == 0 {
		ctx := Analyze(buffer, len(buffer), world)
		inst["op"] = "queue.preview"
		inst["cursorKind"] = ctx.Kind
		if len(ctx.MissingKeys) > 0 {
			inst["missing"] = ctx.MissingKeys
		}
	} else if _, ok := inst["op"]; !ok {
		inst["op"] = "queue.preview"
	}
	return CompileDraft{Kind: "accepted.instruction", Queue: "instruction.jsonl", Instruction: inst, Reason: "line accepted by human"}
}

func Apply(buffer string, edit TextEdit) string {
	if edit.Start < 0 {
		edit.Start = 0
	}
	if edit.End < edit.Start {
		edit.End = edit.Start
	}
	if edit.Start > len(buffer) {
		edit.Start = len(buffer)
	}
	if edit.End > len(buffer) {
		edit.End = len(buffer)
	}
	return buffer[:edit.Start] + edit.Text + buffer[edit.End:]
}

func sortSuggestions(s []Suggestion) {
	sort.SliceStable(s, func(i, j int) bool {
		if s[i].Score != s[j].Score {
			return s[i].Score > s[j].Score
		}
		return s[i].Label < s[j].Label
	})
}

func keyEditRange(buffer string, cursor int) (int, int) {
	start := cursor
	for start > 0 {
		c := buffer[start-1]
		if c == '{' || c == ',' || c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			break
		}
		start--
	}
	return start, cursor
}

func valueEditRange(buffer string, cursor int) (int, int) {
	start := cursor
	for start > 0 {
		c := buffer[start-1]
		if c == ':' || c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			break
		}
		start--
	}
	return start, cursor
}

func cleanPartial(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "\"")
	s = strings.TrimSuffix(s, "\"")
	return s
}

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func detailForKey(k SchemaKey) string {
	parts := []string{}
	if k.Required {
		parts = append(parts, "required")
	} else {
		parts = append(parts, "optional")
	}
	if k.Type != "" {
		parts = append(parts, k.Type)
	}
	return strings.Join(parts, " | ")
}

func tag(group, fallback string) string {
	if group == "" {
		return fallback
	}
	return group
}

func match(candidate, partial string) bool {
	c := strings.ToLower(candidate)
	p := strings.ToLower(partial)
	if strings.Contains(c, p) {
		return true
	}
	// cheap subsequence fuzzy match
	j := 0
	for i := 0; i < len(c) && j < len(p); i++ {
		if c[i] == p[j] {
			j++
		}
	}
	return j == len(p)
}
