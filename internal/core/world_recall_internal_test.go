package core

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestWorldRecallIndexIdentityLiteralGolden(t *testing.T) {
	world := WorldRef{WorldID: "world.test", Digest: "sha256:" + strings.Repeat("a", 64)}
	indexID, err := worldRecallIndexID(world.Digest)
	if err != nil {
		t.Fatal(err)
	}
	if want := "sha256:4f22416f3fa08cf02fd2a664b2352f631fee093c58187b7fa0f12a13539f38bb"; indexID != want {
		t.Fatalf("index id=%q", indexID)
	}
}

func TestWorldRecallOverlappingValueSourcesMergeWithSortedUnionRefs(t *testing.T) {
	defaultValue := CommandValue{Value: "safe"}
	world := recallCoreWorld(t, CommandDefinition{
		Kind: CommandInputKind, CommandID: "command.run", CommandVersion: "v1",
		Name: "test.run", Instruction: map[string]any{"op": "proof"},
		Fields: []CommandField{{
			Name: "mode", Type: "enum", Required: true, Enum: []string{"safe"},
			Default: &defaultValue, Examples: []string{"safe"},
			MaterializedValues: []CommandValue{{Value: "safe"}}, Bind: "payload.mode",
		}},
	})
	var fieldIdentity WorldRecallCandidateIdentity
	identifier := func(identity WorldRecallCandidateIdentity) (string, error) {
		if identity.CandidateKind == WorldRecallFieldValueKind {
			fieldIdentity = identity
		}
		return recallCoreIdentifier(identity)
	}
	index, err := PrepareWorldRecall(world, identifier)
	if err != nil {
		t.Fatal(err)
	}
	wantKinds := []string{"field.default", "field.enum", "field.example", "field.materialized"}
	var values []recallCandidate
	for _, candidate := range index.candidates {
		if candidate.Kind == WorldRecallFieldValueKind {
			values = append(values, candidate)
		}
	}
	if len(values) != 1 {
		t.Fatalf("grouped field values=%#v", values)
	}
	gotKinds := make([]string, len(values[0].StructuralRefs))
	for index, ref := range values[0].StructuralRefs {
		gotKinds[index] = ref.TermKind
		if !strings.Contains(ref.TermPath, `command["test.run"].field["mode"]`) {
			t.Fatalf("noncanonical structural path=%q", ref.TermPath)
		}
	}
	if !reflect.DeepEqual(gotKinds, wantKinds) {
		t.Fatalf("structural refs=%v", values[0].StructuralRefs)
	}
	if fieldIdentity.Kind != WorldRecallCandidateIDKind || fieldIdentity.CandidateKind != WorldRecallFieldValueKind ||
		fieldIdentity.World != values[0].World || fieldIdentity.Command != values[0].Command ||
		fieldIdentity.Materialization != values[0].Materialization ||
		!reflect.DeepEqual(fieldIdentity.StructuralRefs, values[0].StructuralRefs) {
		t.Fatalf("identifier material=%#v candidate=%#v", fieldIdentity, values[0])
	}
	if strings.Contains(values[0].Detail, "field.value") || !strings.Contains(values[0].Detail, "field.materialized") {
		t.Fatalf("source detail=%q", values[0].Detail)
	}
	results := index.Recall(WorldRecallQuery{Scope: WorldRecallFieldValue, CommandName: "test.run", FieldName: "mode", Text: "safe"})
	if len(results) != 1 || results[0].Matches[0].TermKind != "field.enum" {
		t.Fatalf("deterministic primary evidence=%#v", results)
	}
}

func TestWorldRecallExplicitPresetIDsRemainDistinct(t *testing.T) {
	world := recallCoreWorld(t, CommandDefinition{
		Kind: CommandInputKind, CommandID: "command.run", CommandVersion: "v1",
		Name: "test.run", Instruction: map[string]any{"op": "proof"},
		Fields: []CommandField{{Name: "mode", Type: "string", Bind: "payload.mode"}},
		Presets: []CommandPreset{
			{ID: "one", Label: "same", Values: map[string]CommandValue{"mode": {Value: "safe"}}},
			{ID: "two", Label: "same", Values: map[string]CommandValue{"mode": {Value: "safe"}}},
		},
	})
	index, err := PrepareWorldRecall(world, recallCoreIdentifier)
	if err != nil {
		t.Fatal(err)
	}
	var presets []recallCandidate
	for _, candidate := range index.candidates {
		if candidate.Kind == WorldRecallObjectPreset {
			presets = append(presets, candidate)
		}
	}
	if len(presets) != 2 || presets[0].CandidateID == presets[1].CandidateID || presets[0].Materialization != presets[1].Materialization {
		t.Fatalf("presets=%#v", presets)
	}
	for _, candidate := range presets {
		foundPresetID := false
		for _, ref := range candidate.StructuralRefs {
			if strings.Contains(ref.TermPath, `preset["one"]`) || strings.Contains(ref.TermPath, `preset["two"]`) {
				foundPresetID = true
			}
		}
		if !foundPresetID {
			t.Fatalf("preset identity path missing: %#v", candidate.StructuralRefs)
		}
	}
}

func TestWorldRecallPreparationFailsClosedOnInvalidAndDuplicateIdentities(t *testing.T) {
	valid := recallCoreWorld(t, CommandDefinition{
		Kind: CommandInputKind, CommandID: "command.run", CommandVersion: "v1",
		Name: "test.run", Instruction: map[string]any{"op": "proof"},
		Fields: []CommandField{{Name: "mode", Type: "string", Bind: "payload.mode"}},
	})
	tests := []struct {
		name       string
		world      *JsonlWorld
		identifier WorldRecallCandidateIdentifier
	}{
		{"nil identifier", valid, nil},
		{"identifier error", valid, func(WorldRecallCandidateIdentity) (string, error) { return "", errors.New("identity unavailable") }},
		{"invalid candidate identity", valid, func(WorldRecallCandidateIdentity) (string, error) { return "not-a-digest", nil }},
		{"duplicate candidate identity", valid, func(WorldRecallCandidateIdentity) (string, error) { return "sha256:" + strings.Repeat("d", 64), nil }},
		{"nil world", nil, recallCoreIdentifier},
		{"invalid world digest", func() *JsonlWorld { value := *valid; value.Digest = "sha256:bad"; return &value }(), recallCoreIdentifier},
		{"stale world digest", func() *JsonlWorld {
			value := *valid
			value.Commands = append([]CommandDefinition(nil), valid.Commands...)
			value.Commands[0].Description = "mutated"
			return &value
		}(), recallCoreIdentifier},
		{"invalid command identity", func() *JsonlWorld {
			value := *valid
			value.Commands = append([]CommandDefinition(nil), valid.Commands...)
			value.Commands[0].CommandID = "bad id"
			value.RecomputeDigest()
			return &value
		}(), recallCoreIdentifier},
		{"duplicate generated identity", func() *JsonlWorld {
			value := *valid
			value.Commands = append([]CommandDefinition(nil), valid.Commands...)
			value.Commands[0].Fields = append(value.Commands[0].Fields, value.Commands[0].Fields[0])
			value.RecomputeDigest()
			return &value
		}(), recallCoreIdentifier},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			index, err := PrepareWorldRecall(test.world, test.identifier)
			if err == nil || index != nil {
				t.Fatalf("index=%#v err=%v", index, err)
			}
		})
	}
}

func TestWorldRecallPreparesFoldedTermsOutsideRecall(t *testing.T) {
	world := recallCoreWorld(t, CommandDefinition{
		Kind: CommandInputKind, CommandID: "command.case", CommandVersion: "v1",
		Name: "Case.Command", Keywords: []string{"ÄBC"}, Instruction: map[string]any{"op": "proof"},
	})
	index, err := PrepareWorldRecall(world, recallCoreIdentifier)
	if err != nil {
		t.Fatal(err)
	}
	for candidateIndex := range index.candidates {
		for termIndex := range index.candidates[candidateIndex].Terms {
			term := &index.candidates[candidateIndex].Terms[termIndex]
			if term.Folded != strings.ToLower(term.Text) {
				t.Fatalf("unprepared term=%#v", term)
			}
			term.Text = "changed-after-prepare"
		}
	}
	results := index.Recall(WorldRecallQuery{Scope: WorldRecallObjectQuery, Text: "äbc"})
	if len(results) != 1 || len(results[0].Matches) != 1 {
		t.Fatalf("recall did not use prepared folded term: %#v", results)
	}
}

func TestWorldRecallCanonicalComponentsPreventBracketAndNULCollisions(t *testing.T) {
	left := recallTypedPath("field", "a].field[b\x00c")
	right := recallTypedPath("field", "a\x00b].field[c")
	if left == right || strings.ContainsRune(left, '\x00') || strings.ContainsRune(right, '\x00') {
		t.Fatalf("colliding/noncanonical paths left=%q right=%q", left, right)
	}
	if !strings.Contains(left, `\u0000`) || !strings.HasPrefix(left, `field["`) {
		t.Fatalf("path is not JSON-component encoded=%q", left)
	}
}

func recallCoreWorld(t *testing.T, commands ...CommandDefinition) *JsonlWorld {
	t.Helper()
	world := &JsonlWorld{
		Identity: &WorldDefinition{Kind: WorldDefinitionKind, WorldID: "world.test"},
		Commands: commands,
	}
	if err := world.RecomputeDigest(); err != nil {
		t.Fatal(err)
	}
	return world
}

func recallCoreIdentifier(identity WorldRecallCandidateIdentity) (string, error) {
	return CanonicalDigest(identity)
}
