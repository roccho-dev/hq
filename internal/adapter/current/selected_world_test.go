package current

import (
	"reflect"
	"strings"
	"testing"
)

const selectedManifest = `{"kind":"hq.world.v1","world_id":"world.proof"}`
const selectedCommandA = `{"kind":"hq.command.v1","command_id":"command.alpha","command_version":"1","name":"alpha","instruction":{"version":"instruction.v1","op":"run","target":"sh","payload":{"argv":["/bin/alpha"]}}}`
const selectedCommandB = `{"kind":"hq.command.v1","command_id":"command.beta","command_version":"2","name":"beta","instruction":{"version":"instruction.v1","op":"run","target":"sh","payload":{"argv":["/bin/beta"]}}}`

func TestSelectedWorldDigestIgnoresJSONLRowOrder(t *testing.T) {
	first, err := LoadSelectedWorldJSONL(strings.NewReader(strings.Join([]string{selectedManifest, selectedCommandB, selectedCommandA}, "\n")))
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadSelectedWorldJSONL(strings.NewReader(strings.Join([]string{selectedCommandA, selectedManifest, selectedCommandB}, "\n")))
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest != second.Digest || !reflect.DeepEqual(first, second) {
		t.Fatalf("row order changed selected world:\nfirst=%#v\nsecond=%#v", first, second)
	}
	if ref, ok := first.SelectedRef(); !ok || ref.WorldID != "world.proof" || ref.Digest == "" {
		t.Fatalf("selected ref=%+v ok=%v", ref, ok)
	}
	if ref, ok := first.CommandRef("alpha"); !ok || ref.CommandID != "command.alpha" || ref.CommandVersion != "1" || ref.Digest == "" {
		t.Fatalf("command ref=%+v ok=%v", ref, ok)
	}
}

func TestSelectedWorldRejectsMissingOrContradictoryIdentity(t *testing.T) {
	for name, input := range map[string]string{
		"missing manifest": selectedCommandA,
		"duplicate manifest": selectedManifest + "\n" + selectedManifest + "\n" + selectedCommandA,
		"missing command identity": selectedManifest + "\n" + strings.Replace(selectedCommandA, `"command_id":"command.alpha","command_version":"1",`, "", 1),
		"half command identity": selectedManifest + "\n" + strings.Replace(selectedCommandA, `"command_version":"1",`, "", 1),
		"duplicate command id": selectedManifest + "\n" + selectedCommandA + "\n" + strings.Replace(selectedCommandB, `"command_id":"command.beta"`, `"command_id":"command.alpha"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadSelectedWorldJSONL(strings.NewReader(input)); err == nil {
				t.Fatalf("accepted invalid selected world: %s", input)
			}
		})
	}
}

func TestRuntimeLoaderKeepsLegacyWorldWithoutSelectedClaim(t *testing.T) {
	world, err := LoadRuntimeWorldJSONL(strings.NewReader(`{"key":"op","type":"string","required":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if world.Digest == "" {
		t.Fatal("legacy world has no diagnostic digest")
	}
	if _, selected := world.SelectedRef(); selected {
		t.Fatal("legacy world incorrectly claimed selected identity")
	}
}
