package hq

import (
	"encoding/json"
	"sort"
	"strings"
)

type CursorKind string

const (
	CursorKey   CursorKind = "key"
	CursorValue CursorKind = "value"
	CursorRaw   CursorKind = "raw"
)

// CursorContext is deliberately tiny: it captures where the cursor is, not UI state.
type CursorContext struct {
	Buffer       string     `json:"buffer"`
	Cursor       int        `json:"cursor"`
	Kind         CursorKind `json:"kind"`
	Partial      string     `json:"partial"`
	ActiveKey    string     `json:"activeKey,omitempty"`
	PresentKeys  []string   `json:"presentKeys,omitempty"`
	MissingKeys  []string   `json:"missingKeys,omitempty"`
	InsideString bool       `json:"insideString"`
}

func Analyze(buffer string, cursor int, world *JsonlWorld) CursorContext {
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(buffer) {
		cursor = len(buffer)
	}
	before := buffer[:cursor]
	present := keysPresent(buffer)
	ctx := CursorContext{
		Buffer:       buffer,
		Cursor:       cursor,
		Kind:         CursorKey,
		Partial:      keyPartial(before),
		PresentKeys:  present,
		MissingKeys:  missingRequired(present, world),
		InsideString: insideString(before),
	}

	if key := activeValueKey(before); key != "" {
		ctx.Kind = CursorValue
		ctx.ActiveKey = key
		ctx.Partial = valuePartial(before)
		return ctx
	}

	trimmed := strings.TrimSpace(before)
	if trimmed == "" || strings.HasSuffix(trimmed, "{") || strings.HasSuffix(trimmed, ",") || ctx.InsideString {
		ctx.Kind = CursorKey
		ctx.Partial = keyPartial(before)
		return ctx
	}

	// If a colon has appeared after the last comma/object-open and we could not
	// resolve the key, treat it as raw value-ish text rather than proposing keys.
	segment := afterLastAny(before, []string{"{", ","})
	if strings.Contains(segment, ":") {
		ctx.Kind = CursorRaw
		ctx.Partial = strings.TrimSpace(segment)
	}
	return ctx
}

func missingRequired(present []string, world *JsonlWorld) []string {
	if world == nil {
		return nil
	}
	m := map[string]bool{}
	for _, k := range present {
		m[k] = true
	}
	out := make([]string, 0)
	for _, k := range world.Keys {
		if k.Required && !m[k.Key] {
			out = append(out, k.Key)
		}
	}
	sort.Strings(out)
	return out
}

func keysPresent(buffer string) []string {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(buffer), &obj); err == nil {
		keys := make([]string, 0, len(obj))
		for k := range obj {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return keys
	}

	// Fallback for partial JSON: scan quoted keys followed by a colon.
	keys := map[string]bool{}
	in := false
	esc := false
	start := -1
	for i, r := range buffer {
		if esc {
			esc = false
			continue
		}
		if r == '\\' && in {
			esc = true
			continue
		}
		if r == '"' {
			if in {
				text := buffer[start:i]
				rest := strings.TrimSpace(buffer[i+1:])
				if strings.HasPrefix(rest, ":") {
					keys[text] = true
				}
				in = false
			} else {
				in = true
				start = i + 1
			}
		}
	}
	out := make([]string, 0, len(keys))
	for k := range keys {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func activeValueKey(before string) string {
	seg := afterLastAny(before, []string{"{", ","})
	colon := strings.LastIndex(seg, ":")
	if colon < 0 {
		return ""
	}
	left := strings.TrimSpace(seg[:colon])
	left = strings.Trim(left, "\" ")
	if left == "" || strings.Contains(left, " ") {
		return ""
	}
	return left
}

func keyPartial(before string) string {
	seg := afterLastAny(before, []string{"{", ","})
	seg = strings.TrimSpace(seg)
	seg = strings.TrimPrefix(seg, "\"")
	return seg
}

func valuePartial(before string) string {
	seg := afterLastAny(before, []string{":"})
	seg = strings.TrimSpace(seg)
	seg = strings.TrimPrefix(seg, "\"")
	return seg
}

func insideString(s string) bool {
	in := false
	esc := false
	for _, r := range s {
		if esc {
			esc = false
			continue
		}
		if r == '\\' && in {
			esc = true
			continue
		}
		if r == '"' {
			in = !in
		}
	}
	return in
}

func afterLastAny(s string, seps []string) string {
	idx := -1
	sepLen := 0
	for _, sep := range seps {
		if i := strings.LastIndex(s, sep); i >= 0 && i >= idx {
			idx = i
			sepLen = len(sep)
		}
	}
	if idx < 0 {
		return s
	}
	return s[idx+sepLen:]
}
