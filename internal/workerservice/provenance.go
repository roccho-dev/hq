package workerservice

import (
	"fmt"
	"os"

	"hq/internal/adapter/current"
	"hq/internal/core"
	"hq/internal/hqprofile"
	"hq/internal/worker"
)

func loadProfileWorld(profile hqprofile.Profile) (*core.JsonlWorld, error) {
	if profile.WorldPath == "" {
		return nil, nil
	}
	file, err := os.Open(profile.WorldPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return current.LoadRuntimeWorldJSONL(file)
}

// validateSelectedWorldRows converts provenance mismatch into the normal typed
// per-row validation path. Rows without provenance remain legacy-compatible but
// make no selected-world or history claim.
func validateSelectedWorldRows(rows []worker.ReadRow, world *core.JsonlWorld) []worker.ReadRow {
	out := append([]worker.ReadRow(nil), rows...)
	worldRef, selected := world.SelectedRef()
	for i := range out {
		provenance := out[i].Provenance
		if provenance == nil || out[i].ParseError != nil {
			continue
		}
		if !selected {
			out[i].ParseError = provenanceDiagnostic("selected world has no explicit identity")
			continue
		}
		if provenance.World != worldRef {
			out[i].ParseError = provenanceDiagnostic(fmt.Sprintf("accepted world %s@%s does not match selected world %s@%s", provenance.World.WorldID, provenance.World.Digest, worldRef.WorldID, worldRef.Digest))
			continue
		}
		if provenance.Command == nil {
			continue
		}
		command, ok := world.CommandByID(provenance.Command.CommandID)
		if !ok {
			out[i].ParseError = provenanceDiagnostic("accepted command_id is absent from selected world")
			continue
		}
		currentRef, ok := world.CommandRef(command.Name)
		if !ok || currentRef != *provenance.Command {
			out[i].ParseError = provenanceDiagnostic("accepted command contract does not match selected world")
		}
	}
	return out
}

func provenanceDiagnostic(message string) *worker.Diagnostic {
	return &worker.Diagnostic{Code: "selected_world_mismatch", Field: "provenance", Message: message}
}
