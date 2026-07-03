package core

import "sort"

// SchemaKey describes one logical JSONL key that the autocomplete compiler may propose.
// It is a core protocol type, not a concrete source-file schema parser.
type SchemaKey struct {
	Key         string   `json:"key"`
	Type        string   `json:"type,omitempty"`
	Required    bool     `json:"required,omitempty"`
	Description string   `json:"description,omitempty"`
	Group       string   `json:"group,omitempty"`
	Enum        []string `json:"enum,omitempty"`
	Examples    []string `json:"examples,omitempty"`
}

// JsonlWorld is the schema-independent domain model used by hq core.
// Adapters translate concrete files into this type before calling core.
type JsonlWorld struct {
	Keys []SchemaKey      `json:"keys"`
	Rows []map[string]any `json:"rows,omitempty"`
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
