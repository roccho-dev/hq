package workeraccept

import (
	"encoding/json"
	"strings"
	"testing"

	"hq/internal/core"
)

func TestAcceptedCompileProvenanceBindsInstruction(t *testing.T) {
	var instruction any
	if err := json.Unmarshal([]byte(canonicalInstruction), &instruction); err != nil {
		t.Fatal(err)
	}
	instructionDigest, err := core.CanonicalDigest(instruction)
	if err != nil {
		t.Fatal(err)
	}
	worldDigest, _ := core.CanonicalDigest("world")
	provenance := core.CompileProvenance{
		Kind: core.CompileProvenanceKind, InputKind: core.CanonicalJSONInputKind,
		World: core.WorldRef{WorldID: "world.proof", Digest: worldDigest},
		InstructionDigest: instructionDigest,
	}
	envelope := map[string]any{
		"kind": AcceptedKind, "queue": AcceptedQueue,
		"instruction": instruction, "provenance": provenance,
	}
	encoded, _ := json.Marshal(envelope)
	rows, err := Read("accepted.jsonl", strings.NewReader(string(encoded)+"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ParseError != nil || rows[0].Provenance == nil {
		t.Fatalf("valid provenance rejected: %+v", rows)
	}

	provenance.InstructionDigest = worldDigest
	envelope["provenance"] = provenance
	encoded, _ = json.Marshal(envelope)
	rows, err = Read("accepted.jsonl", strings.NewReader(string(encoded)+"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].ParseError == nil || rows[0].ParseError.Code != "compile_provenance_mismatch" {
		t.Fatalf("mismatch was not rejected: %+v", rows[0])
	}
}
