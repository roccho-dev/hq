package hq

import (
	"encoding/json"
	"errors"

	"hq/internal/core"
)

// CommandObjectRange is the exact byte range of one parsed @command object in
// the submitted document. It is transient editor protocol data, not durable
// instruction meaning.
type CommandObjectRange struct {
	StartByte int
	EndByte   int
}

func CompileSelectedCommandObject(text string, cursorLine int, world *JsonlWorld) (CompileDraft, error) {
	draft, _, err := CompileSelectedCommandObjectWithRange(text, cursorLine, world)
	return draft, err
}

// CompileSelectedCommandObjectWithRange lowers the selected @command object
// and returns the range owned by the same parser. Callers can therefore remove
// exactly the accepted draft without parsing hq syntax independently.
func CompileSelectedCommandObjectWithRange(text string, cursorLine int, world *JsonlWorld) (CompileDraft, CommandObjectRange, error) {
	draft, err := CompileCommandObject(text, cursorLine, world)
	if err != nil {
		return CompileDraft{}, CommandObjectRange{}, err
	}
	lines := documentLines(text)
	object, err := parseCommandObject(lines, cursorLine)
	if err != nil {
		return CompileDraft{}, CommandObjectRange{}, err
	}
	endByte := len(text)
	if object.EndLine < len(lines) {
		endByte = lines[object.EndLine].Start
	}
	objectRange := CommandObjectRange{
		StartByte: lines[object.StartLine].Start,
		EndByte:   endByte,
	}
	if world == nil {
		return draft, objectRange, nil
	}
	worldRef, selected := world.SelectedRef()
	if !selected {
		return draft, objectRange, nil
	}
	commandRef, ok := world.CommandRef(object.Name)
	if !ok {
		return CompileDraft{}, CommandObjectRange{}, errors.New("selected command has no stable identity/version")
	}
	draft.Provenance = &CompileProvenance{
		Kind:      CompileProvenanceKind,
		InputKind: CommandInputKind,
		World:     worldRef,
		Command:   &commandRef,
	}
	definition, _ := world.Command(object.Name)
	acceptedFields := make([]core.AcceptedInputField, 0, len(object.Values))
	for _, field := range definition.Fields {
		raw, supplied := object.Values[field.Name]
		if !supplied || !core.HistoryPolicyPersists(field) {
			continue
		}
		value, valueErr := commandValue(field, raw)
		if valueErr != nil {
			return CompileDraft{}, CommandObjectRange{}, valueErr
		}
		if field.Type == "integer" {
			value = json.Number(raw)
		}
		acceptedFields = append(acceptedFields, core.AcceptedInputField{Name: field.Name, Type: field.Type, Value: value})
	}
	draft.AcceptedInput = &core.AcceptedInput{
		Kind:               core.AcceptedInputKind,
		World:              worldRef,
		Command:            commandRef,
		Fields:             acceptedFields,
		SuppliedFieldCount: len(object.Values),
		RecallComplete:     len(acceptedFields) == len(object.Values),
		RenderContract:     core.CommandObjectMaterializerVersion,
	}
	if err := draft.AcceptedInput.ApplyMaterializationLimit(); err != nil {
		return CompileDraft{}, CommandObjectRange{}, err
	}
	return draft, objectRange, nil
}

func BindFinalInstructionProvenance(draft *CompileDraft, world *JsonlWorld, inputKind string) error {
	if draft == nil || draft.Instruction == nil {
		return errors.New("compile draft has no instruction")
	}
	worldRef, selected := world.SelectedRef()
	if !selected {
		draft.Provenance = nil
		draft.AcceptedInput = nil
		return nil
	}
	if draft.Provenance == nil {
		draft.Provenance = &CompileProvenance{
			Kind:      CompileProvenanceKind,
			InputKind: inputKind,
			World:     worldRef,
		}
	} else if draft.Provenance.World != worldRef {
		return errors.New("compile draft provenance does not match selected world")
	}
	digest, err := core.CanonicalDigest(draft.Instruction)
	if err != nil {
		return err
	}
	draft.Provenance.InstructionDigest = digest
	if err := draft.Provenance.Validate(); err != nil {
		return err
	}
	if draft.AcceptedInput != nil {
		if draft.Provenance.Command == nil || draft.AcceptedInput.World != draft.Provenance.World || draft.AcceptedInput.Command != *draft.Provenance.Command {
			return errors.New("accepted input does not match compile provenance")
		}
		if err := draft.AcceptedInput.BindInstructionDigest(digest); err != nil {
			return err
		}
	}
	return nil
}
