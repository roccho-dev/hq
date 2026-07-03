package core

type TextEdit struct {
	Start int    `json:"start"`
	End   int    `json:"end"`
	Text  string `json:"text"`
}

type CompileDraft struct {
	Kind        string         `json:"kind"`
	Queue       string         `json:"queue"`
	Key         string         `json:"key,omitempty"`
	Value       any            `json:"value,omitempty"`
	Instruction map[string]any `json:"instruction"`
	Reason      string         `json:"reason,omitempty"`
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
