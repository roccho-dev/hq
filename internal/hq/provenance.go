package hq

import (
	"errors"

	"hq/internal/core"
)

func CompileSelectedCommandObject(text string, cursorLine int, world *JsonlWorld) (CompileDraft, error) {
	draft, err := CompileCommandObject(text, cursorLine, world)
	if err != nil {
		return CompileDraft{}, err
	}
	if world == nil {
		return draft, nil
	}
	worldRef, selected := world.SelectedRef()
	if !selected {
		return draft, nil
	}
	object, err := parseCommandObject(documentLines(text), cursorLine)
	if err != nil {
		return CompileDraft{}, err
	}
	commandRef, ok := world.CommandRef(object.Name)
	if !ok {
		return CompileDraft{}, errors.New("selected command has no stable identity/version")
	}
	draft.Provenance = &CompileProvenance{
		Kind: CompileProvenanceKind,
		InputKind: CommandInputKind,
		World: worldRef,
		Command: &commandRef,
	}
	return draft, nil
}

func BindFinalInstructionProvenance(draft *CompileDraft, world *JsonlWorld, inputKind string) error {
	if draft == nil || draft.Instruction == nil {
		return errors.New("compile draft has no instruction")
	}
	worldRef, selected := world.SelectedRef()
	if !selected {
		draft.Provenance = nil
		return nil
	}
	if draft.Provenance == nil {
		draft.Provenance = &CompileProvenance{
			Kind: CompileProvenanceKind,
			InputKind: inputKind,
			World: worldRef,
		}
	} else if draft.Provenance.World != worldRef {
		return errors.New("compile draft provenance does not match selected world")
	}
	digest, err := core.CanonicalDigest(draft.Instruction)
	if err != nil {
		return err
	}
	draft.Provenance.InstructionDigest = digest
	return draft.Provenance.Validate()
}
