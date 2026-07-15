package current

import (
	"strings"
	"testing"
)

func TestSelectedWorldHistoryPolicyValidation(t *testing.T) {
	identity := `{"kind":"hq.world.v1","world_id":"history.policy"}`
	valid := `{"kind":"hq.command.v1","command_id":"demo","command_version":"1","name":"demo","instruction":{"version":"instruction.v1","op":"run","target":"demo","payload":{}},"fields":[{"name":"safe","type":"string","history_policy":"search","bind":"payload.safe"},{"name":"secret","type":"string","sensitive":true,"bind":"payload.secret"}]}`
	if _, err := LoadSelectedWorldJSONL(strings.NewReader(identity + "\n" + valid)); err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"unknown policy": strings.Replace(valid, `"history_policy":"search"`, `"history_policy":"other"`, 1),
		"sensitive recall": strings.Replace(valid, `"sensitive":true`, `"sensitive":true,"history_policy":"recall"`, 1),
		"sensitive example": strings.Replace(valid, `"sensitive":true`, `"sensitive":true,"examples":["leak"]`, 1),
		"sensitive preset": strings.Replace(valid, `}]}`, `}],"presets":[{"id":"bad","label":"Bad","values":{"secret":"leak"}}]}`, 1),
	}
	for name, row := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadSelectedWorldJSONL(strings.NewReader(identity + "\n" + row)); err == nil {
				t.Fatalf("selected world accepted invalid history policy: %s", row)
			}
		})
	}
}
