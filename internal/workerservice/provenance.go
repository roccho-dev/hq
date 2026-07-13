package workerservice

import (
	"errors"
	"fmt"
	"os"

	"hq/internal/adapter/current"
	"hq/internal/core"
	"hq/internal/hqprofile"
	"hq/internal/localtool"
	"hq/internal/worker"
	"hq/internal/worker/adapter"
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

// loadRegistryForWorld builds adapters from the exact in-memory world snapshot
// already used for provenance validation. This removes a profile-replacement
// race between validation and provider preparation.
func loadRegistryForWorld(profile hqprofile.Profile, world *core.JsonlWorld) (*adapter.Registry, error) {
	if world == nil {
		return hostRegistry(profile)
	}
	registrations := []adapter.Registration{}
	if worldSelectsTarget(world, "host") {
		if profile.CapabilitiesPath == "" {
			return nil, errors.New("selected world declares host commands but profile has no capabilities_path")
		}
		host, err := hostRegistration(profile)
		if err != nil {
			return nil, err
		}
		registrations = append(registrations, host)
	}
	if len(world.LocalTools) != 0 {
		if profile.ExecutableBindingsPath == "" {
			return nil, errors.New("selected world declares local tools but profile has no executable_bindings_path")
		}
		registrations = append(registrations, adapter.Registration{Target: "local-tool", Preparer: localtool.Preparer{World: world, BindingsPath: profile.ExecutableBindingsPath}})
	}
	return adapter.NewRegistry(registrations...)
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
