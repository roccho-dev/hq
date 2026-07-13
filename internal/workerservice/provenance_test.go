package workerservice

import (
	"strings"
	"testing"

	"hq/internal/adapter/current"
	"hq/internal/core"
	"hq/internal/worker"
)

func TestSelectedWorldProvenanceExactMatchAndMismatch(t *testing.T) {
	world := selectedWorldFixture(t, "world.runtime", "1")
	worldRef, _ := world.SelectedRef()
	commandRef, _ := world.CommandRef("proof.run")
	matching := worker.ReadRow{Provenance: &core.CompileProvenance{
		Kind: core.CompileProvenanceKind, InputKind: core.CommandInputKind,
		World: worldRef, Command: &commandRef, InstructionDigest: digestFixture(),
	}}
	rows := validateSelectedWorldRows([]worker.ReadRow{matching}, world)
	if rows[0].ParseError != nil {
		t.Fatalf("exact provenance rejected: %+v", rows[0].ParseError)
	}

	wrongWorld := matching
	wrongWorld.Provenance = cloneProvenance(matching.Provenance)
	wrongWorld.Provenance.World.Digest = digestOf("other world")
	rows = validateSelectedWorldRows([]worker.ReadRow{wrongWorld}, world)
	assertSelectedWorldMismatch(t, rows[0])

	wrongCommand := matching
	wrongCommand.Provenance = cloneProvenance(matching.Provenance)
	wrongCommand.Provenance.Command.Digest = digestOf("other command")
	rows = validateSelectedWorldRows([]worker.ReadRow{wrongCommand}, world)
	assertSelectedWorldMismatch(t, rows[0])
}

func TestLegacyRowRemainsCompatibleWithoutSelectedClaim(t *testing.T) {
	world := selectedWorldFixture(t, "world.runtime", "1")
	legacy := worker.ReadRow{Instruction: worker.Instruction{ID: "legacy"}}
	rows := validateSelectedWorldRows([]worker.ReadRow{legacy}, world)
	if rows[0].ParseError != nil || rows[0].Provenance != nil {
		t.Fatalf("legacy row changed: %+v", rows[0])
	}
}

func selectedWorldFixture(t *testing.T, worldID, commandVersion string) *core.JsonlWorld {
	t.Helper()
	input := `{"kind":"hq.world.v1","world_id":"` + worldID + `"}` + "\n" +
		`{"kind":"hq.command.v1","command_id":"proof.run","command_version":"` + commandVersion + `","name":"proof.run","instruction":{"version":"instruction.v1","op":"run","target":"sh","payload":{"argv":["proof"]}}}`
	world, err := current.LoadSelectedWorldJSONL(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	return world
}

func digestFixture() string { return digestOf("instruction") }

func digestOf(value string) string {
	digest, _ := core.CanonicalDigest(value)
	return digest
}

func cloneProvenance(value *core.CompileProvenance) *core.CompileProvenance {
	copy := *value
	if value.Command != nil {
		command := *value.Command
		copy.Command = &command
	}
	return &copy
}

func assertSelectedWorldMismatch(t *testing.T, row worker.ReadRow) {
	t.Helper()
	if row.ParseError == nil || row.ParseError.Code != "selected_world_mismatch" {
		t.Fatalf("row did not fail closed: %+v", row)
	}
}
