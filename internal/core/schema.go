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

// CommandField declares one human-facing command argument and where its value
// is lowered into the canonical instruction object. The compiler interprets
// only these structural declarations; command meaning remains world data.
type CommandField struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Required    bool     `json:"required,omitempty"`
	Description string   `json:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"`
	Examples    []string `json:"examples,omitempty"`
	Bind        string   `json:"bind"`
}

// CommandDefinition is an adapter-provided input-language declaration. Base
// instruction fields and argument bindings are data, not compiler semantics.
type CommandDefinition struct {
	Kind           string         `json:"kind"`
	CommandID      string         `json:"command_id,omitempty"`
	CommandVersion string         `json:"command_version,omitempty"`
	Name           string         `json:"name"`
	Description    string         `json:"description,omitempty"`
	Instruction    map[string]any `json:"instruction"`
	Fields         []CommandField `json:"fields,omitempty"`
}

// LocalToolDefinition is a finite, data-only description of one exact local
// tool contract. BindingRef identifies an installed provider; executable
// paths and provider discovery are deliberately absent from the world model.
type LocalToolDefinition struct {
	Kind                   string            `json:"kind"`
	ToolID                 string            `json:"tool_id"`
	ToolVersion            string            `json:"tool_version"`
	BindingRef             string            `json:"binding_ref"`
	BindingContractVersion string            `json:"binding_contract_version"`
	Actions                []LocalToolAction `json:"actions"`
}

type LocalToolAction struct {
	ActionID   string              `json:"action_id"`
	Inputs     []LocalToolInput    `json:"inputs,omitempty"`
	Argv       []LocalToolArg      `json:"argv"`
	Stdin      LocalToolStdin      `json:"stdin"`
	Limits     LocalToolLimits     `json:"limits"`
	Output     LocalToolOutput     `json:"output"`
	NativeRefs LocalToolNativeRefs `json:"native_refs,omitempty"`
	Lifecycle  string              `json:"lifecycle"`
	Risk       string              `json:"risk"`
	Approval   string              `json:"approval"`
}

type LocalToolInput struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Required bool     `json:"required,omitempty"`
	Enum     []string `json:"enum,omitempty"`
}

// LocalToolArg is a strict tagged union: exactly one of Literal or Field must
// be present. Pointers distinguish an absent member from an explicitly empty
// value, which validation rejects independently.
type LocalToolArg struct {
	Literal *string `json:"literal,omitempty"`
	Field   *string `json:"field,omitempty"`
}

type LocalToolStdin struct {
	Mode     string `json:"mode"`
	Field    string `json:"field,omitempty"`
	MaxBytes int    `json:"max_bytes"`
}

type LocalToolLimits struct {
	TimeoutMS   int `json:"timeout_ms"`
	StdoutBytes int `json:"stdout_bytes"`
	StderrBytes int `json:"stderr_bytes"`
}

type LocalToolOutput struct {
	Format string `json:"format"`
}

type LocalToolNativeRefs struct {
	Session *LocalToolNativeSelector `json:"session,omitempty"`
}

// LocalToolNativeSelector is a fixed structured lookup. Path contains literal
// object-field segments only; it is not JSONPath, a regular expression, or an
// executable expression language. Source stdout means the sole JSON value or,
// for JSONL output, the last non-empty record.
type LocalToolNativeSelector struct {
	Source string   `json:"source"`
	Path   []string `json:"path"`
}

// JsonlWorld is the schema-independent domain model used by hq core.
// Adapters translate concrete files into this type before calling core.
type JsonlWorld struct {
	Identity   *WorldDefinition      `json:"identity,omitempty"`
	Digest     string                `json:"digest,omitempty"`
	Keys       []SchemaKey           `json:"keys"`
	Commands   []CommandDefinition   `json:"commands,omitempty"`
	LocalTools []LocalToolDefinition `json:"local_tools,omitempty"`
	Rows       []map[string]any      `json:"rows,omitempty"`
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

func (w *JsonlWorld) Command(name string) (CommandDefinition, bool) {
	if w == nil {
		return CommandDefinition{}, false
	}
	for _, command := range w.Commands {
		if command.Name == name {
			return command, true
		}
	}
	return CommandDefinition{}, false
}

func (w *JsonlWorld) LocalTool(toolID, version string) (LocalToolDefinition, bool) {
	if w == nil {
		return LocalToolDefinition{}, false
	}
	for _, tool := range w.LocalTools {
		if tool.ToolID == toolID && tool.ToolVersion == version {
			return tool, true
		}
	}
	return LocalToolDefinition{}, false
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
