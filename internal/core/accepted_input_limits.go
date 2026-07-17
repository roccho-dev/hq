package core

import "encoding/json"

// ApplyAcceptedInputLimits keeps optional recall evidence bounded without
// rejecting the canonical instruction. The rendered @command object and the
// typed evidence representation must both fit the accepted-input limit.
func (input *AcceptedInput) ApplyAcceptedInputLimits(commandName string) error {
	if input == nil {
		return nil
	}
	encoded, err := json.Marshal(input.Fields)
	if err != nil {
		return err
	}
	rendered := renderAcceptedInput(CommandDefinition{Name: commandName}, input.Fields)
	if len(encoded) > MaxAcceptedInputMaterialization || len(rendered) > MaxAcceptedInputMaterialization {
		input.Fields = []AcceptedInputField{}
		input.RecallComplete = false
	}
	return nil
}
