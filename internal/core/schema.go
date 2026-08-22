package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
)

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
	Name               string         `json:"name"`
	Type               string         `json:"type"`
	Required           bool           `json:"required,omitempty"`
	Description        string         `json:"description,omitempty"`
	Enum               []string       `json:"enum,omitempty"`
	Default            *CommandValue  `json:"default,omitempty"`
	Examples           []string       `json:"examples,omitempty"`
	MaterializedValues []CommandValue `json:"materialized_values,omitempty"`
	HistoryPolicy      string         `json:"history_policy,omitempty"`
	Sensitive          bool           `json:"sensitive,omitempty"`
	Bind               string         `json:"bind"`
}

// CommandValue is one scalar value explicitly materialized by world data. It
// deliberately excludes objects, arrays, and null: hq.command.v1 fields are a
// finite primitive vocabulary, not an embedded expression language.
type CommandValue struct {
	Value any
}

func (v *CommandValue) UnmarshalJSON(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	switch value.(type) {
	case string, bool, json.Number:
		v.Value = value
		return nil
	default:
		return errors.New("command value must be a string, integer, or boolean")
	}
}

func (v CommandValue) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.Value)
}

func (v CommandValue) Text() string {
	switch value := v.Value.(type) {
	case string:
		return value
	case bool:
		return strconv.FormatBool(value)
	case json.Number:
		return value.String()
	case nil:
		return ""
	default:
		return ""
	}
}

// CommandPreset is a complete object explicitly declared by one command row.
// ID is a stable structural component within that exact command definition.
type CommandPreset struct {
	ID     string                  `json:"id"`
	Label  string                  `json:"label"`
	Values map[string]CommandValue `json:"values"`
}

// CommandDefinition is an adapter-provided input-language declaration. Base
// instruction fields and argument bindings are data, not compiler semantics.
type CommandDefinition struct {
	Kind           string          `json:"kind"`
	CommandID      string          `json:"command_id,omitempty"`
	CommandVersion string          `json:"command_version,omitempty"`
	Name           string          `json:"name"`
	Aliases        []string        `json:"aliases,omitempty"`
	Keywords       []string        `json:"keywords,omitempty"`
	Description    string          `json:"description,omitempty"`
	Default        bool            `json:"default,omitempty"`
	Instruction    map[string]any  `json:"instruction"`
	Fields         []CommandField  `json:"fields,omitempty"`
	Presets        []CommandPreset `json:"presets,omitempty"`
}

// LocalToolDefinition is a data-only description of one exact local resource.
// BindingRef identifies an installed provider; executable paths and provider
// discovery are deliberately absent from the world model. Actions are promoted
// typed operations. Invocation is the separately bounded resource-level argv
// policy and does not create per-subcommand world actions.
type LocalToolDefinition struct {
	Kind                   string               `json:"kind"`
	ToolID                 string               `json:"tool_id"`
	ToolVersion            string               `json:"tool_version"`
	BindingRef             string               `json:"binding_ref"`
	BindingContractVersion string               `json:"binding_contract_version"`
	Bindings               []LocalToolBinding   `json:"bindings,omitempty"`
	Invocation             *LocalToolInvocation `json:"invocation,omitempty"`
	Actions                []LocalToolAction    `json:"actions,omitempty"`
}

// LocalToolBinding is one named, exact, one-level executable dependency. It is
// world meaning only: the selected profile still owns the concrete executable.
type LocalToolBinding struct {
	Name                   string `json:"name"`
	BindingRef             string `json:"binding_ref"`
	BindingContractVersion string `json:"binding_contract_version"`
}

// LocalToolInvocation is a selected resource contract, not a command wrapper.
// PolicyVersion is carried in each accepted invocation so exact instruction
// approval cannot silently survive a policy change.
type LocalToolInvocation struct {
	PolicyVersion          string          `json:"policy_version"`
	DeniedOptions          []string        `json:"denied_options,omitempty"`
	DeniedArgumentPrefixes []string        `json:"denied_argument_prefixes,omitempty"`
	MaxArgv                int             `json:"max_argv"`
	MaxArgBytes            int             `json:"max_arg_bytes"`
	Limits                 LocalToolLimits `json:"limits"`
}

type LocalToolAction struct {
	ActionID   string              `json:"action_id"`
	Inputs     []LocalToolInput    `json:"inputs,omitempty"`
	Argv       []LocalToolArg      `json:"argv"`
	Stdin      LocalToolStdin      `json:"stdin"`
	Limits     LocalToolLimits     `json:"limits"`
	Output     LocalToolOutput     `json:"output"`
	NativeRefs LocalToolNativeRefs `json:"native_refs,omitempty"`
	RunView    *LocalToolRunView   `json:"run_view,omitempty"`
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

// LocalToolArg is a strict tagged union. Literal and Field are the original
// finite forms; BindingExecutable inserts one declared verified executable.
type LocalToolArg struct {
	Literal           *string `json:"literal,omitempty"`
	Field             *string `json:"field,omitempty"`
	BindingExecutable *string `json:"binding_executable,omitempty"`
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
	Format string                   `json:"format"`
	Final  *LocalToolNativeSelector `json:"final,omitempty"`
}

type LocalToolNativeRefs struct {
	Session *LocalToolNativeSelector `json:"session,omitempty"`
}

// LocalToolRunView declares a finite, exact provider that must make the run
// visible before the selected action may execute. The provider is world
// meaning; its executable identity remains selected-profile deployment data.
type LocalToolRunView struct {
	Policy      string `json:"policy"`
	ToolID      string `json:"tool_id"`
	ToolVersion string `json:"tool_version"`
	ActionID    string `json:"action_id"`
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
