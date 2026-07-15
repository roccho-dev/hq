package core_test

import (
	"strings"
	"testing"
	"time"

	"hq/internal/core"
)

func TestLongHistoryValuesKeepFieldSpecificCandidateIdentity(t *testing.T) {
	command := core.CommandDefinition{
		Kind: core.CommandInputKind, CommandID: "demo", CommandVersion: "1", Name: "demo",
		Instruction: map[string]any{"version": "instruction.v1", "op": "run", "target": "demo", "payload": map[string]any{}},
		Fields: []core.CommandField{
			{Name: "left", Type: "string", HistoryPolicy: core.HistoryPolicyRecall, Bind: "payload.left"},
			{Name: "right", Type: "string", HistoryPolicy: core.HistoryPolicyRecall, Bind: "payload.right"},
		},
	}
	world := &core.JsonlWorld{Identity: &core.WorldDefinition{Kind: core.WorldDefinitionKind, WorldID: "history.long"}, Commands: []core.CommandDefinition{command}}
	if err := world.RecomputeDigest(); err != nil { t.Fatal(err) }
	identify := func(identity core.WorldRecallCandidateIdentity) (string, error) { return core.CanonicalDigest(identity) }
	base, err := core.PrepareWorldRecall(world, identify)
	if err != nil { t.Fatal(err) }
	worldRef, _ := world.SelectedRef()
	commandRef, _ := world.CommandRef("demo")
	value := strings.Repeat("x", core.MaxHistorySearchRunes+1)
	instructionDigest, _ := core.CanonicalDigest(map[string]any{"id": "long", "created_at": "2026-07-16T00:00:00Z"})
	input := core.AcceptedInput{
		Kind: core.AcceptedInputKind, World: worldRef, Command: commandRef,
		Fields: []core.AcceptedInputField{{Name: "left", Type: "string", Value: value}, {Name: "right", Type: "string", Value: value}},
		SuppliedFieldCount: 2, RecallComplete: true, RenderContract: core.CommandObjectMaterializerVersion,
	}
	if err := input.BindInstructionDigest(instructionDigest); err != nil { t.Fatal(err) }
	at, _ := time.Parse(time.RFC3339, "2026-07-16T00:00:00Z")
	record := core.AcceptedHistoryRecord{AcceptedID: "long", AcceptedAt: at, Provenance: core.CompileProvenance{Kind: core.CompileProvenanceKind, InputKind: core.CommandInputKind, World: worldRef, Command: &commandRef, InstructionDigest: instructionDigest}, Input: input}
	combined, report := core.AttachAcceptedHistory(world, base, []core.AcceptedHistoryRecord{record}, identify)
	if report.Fatal { t.Fatalf("history identity failed: %#v", report) }
	left := combined.Recall(core.WorldRecallQuery{Scope: core.WorldRecallFieldValue, CommandName: "demo", FieldName: "left"})
	right := combined.Recall(core.WorldRecallQuery{Scope: core.WorldRecallFieldValue, CommandName: "demo", FieldName: "right"})
	if len(left) != 1 || len(right) != 1 || left[0].CandidateID == right[0].CandidateID { t.Fatalf("field-specific identity missing: left=%#v right=%#v", left, right) }
	if got := combined.Recall(core.WorldRecallQuery{Scope: core.WorldRecallFieldValue, CommandName: "demo", FieldName: "left", Text: "xxx"}); len(got) != 0 { t.Fatalf("long value became searchable: %#v", got) }
}
