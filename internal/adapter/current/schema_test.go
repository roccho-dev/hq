package current

import (
	"reflect"
	"strings"
	"testing"
)

const validLocalTool = `{"kind":"hq.local-tool.v1","tool_id":"dummy","tool_version":"1","binding_ref":"local-tool.dummy","binding_contract_version":"envctl.verified-executable-binding.v1","actions":[{"action_id":"echo","inputs":[{"name":"value","type":"string","required":true}],"argv":[{"literal":"--echo"},{"field":"value"}],"stdin":{"mode":"none","max_bytes":0},"limits":{"timeout_ms":1000,"stdout_bytes":4096,"stderr_bytes":4096},"output":{"format":"text"},"lifecycle":"one-shot","risk":"low","approval":"explicit"}]}`

const secondLocalTool = `{"kind":"hq.local-tool.v1","tool_id":"alpha","tool_version":"2","binding_ref":"local-tool.alpha","binding_contract_version":"envctl.verified-executable-binding.v1","actions":[{"action_id":"version","argv":[{"literal":"--version"}],"stdin":{"mode":"none","max_bytes":0},"limits":{"timeout_ms":1000,"stdout_bytes":4096,"stderr_bytes":4096},"output":{"format":"json"},"native_refs":{"session":{"source":"stdout","path":["session_id"]}},"lifecycle":"one-shot","risk":"low","approval":"explicit"}]}`

const validLocalToolCommand = `{"kind":"hq.command.v1","name":"dummy.echo","instruction":{"version":"instruction.v1","op":"run","target":"local-tool","payload":{"tool_id":"dummy","tool_version":"1","action_id":"echo","input":{}}},"fields":[{"name":"value","type":"string","required":true,"bind":"payload.input.value"}]}`

func TestLoadLocalToolStrictAndExactLookup(t *testing.T) {
	world, err := LoadSchemaJSONL(strings.NewReader(validLocalTool))
	if err != nil {
		t.Fatal(err)
	}
	tool, ok := world.LocalTool("dummy", "1")
	if !ok || tool.BindingRef != "local-tool.dummy" || tool.BindingContractVersion != "envctl.verified-executable-binding.v1" {
		t.Fatalf("tool=%#v ok=%v", tool, ok)
	}
	if _, ok := world.LocalTool("dummy", "2"); ok {
		t.Fatal("lookup ignored exact tool version")
	}
}

func TestLoadLocalToolRejectsUnknownFieldsAndExecutablePath(t *testing.T) {
	for name, row := range map[string]string{
		"unknown": strings.Replace(validLocalTool, `"tool_id":"dummy"`, `"tool_id":"dummy","mystery":true`, 1),
		"path":    strings.Replace(validLocalTool, `"binding_ref":"local-tool.dummy"`, `"binding_ref":"local-tool.dummy","executable_path":"C:\\tools\\dummy.exe"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadSchemaJSONL(strings.NewReader(row)); err == nil {
				t.Fatalf("strict loader accepted %s", row)
			}
		})
	}
}

func TestLoadLocalToolRejectsDuplicateIdentities(t *testing.T) {
	duplicateAction := strings.Replace(validLocalTool, `"actions":[`, `"actions":[{"action_id":"echo","argv":[{"literal":"x"}],"stdin":{"mode":"none","max_bytes":0},"limits":{"timeout_ms":1,"stdout_bytes":1,"stderr_bytes":1},"output":{"format":"text"},"lifecycle":"one-shot","risk":"low","approval":"explicit"},`, 1)
	duplicateInput := strings.Replace(validLocalTool, `"inputs":[`, `"inputs":[{"name":"value","type":"string"},`, 1)
	for name, data := range map[string]string{
		"tool":   validLocalTool + "\n" + validLocalTool,
		"action": duplicateAction,
		"input":  duplicateInput,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadSchemaJSONL(strings.NewReader(data)); err == nil {
				t.Fatalf("loader accepted duplicate %s", name)
			}
		})
	}
}

func TestLoadLocalToolRejectsInvalidArgvTemplates(t *testing.T) {
	for name, argv := range map[string]string{
		"primitive": `["--echo"]`,
		"both":      `[{"literal":"--echo","field":"value"}]`,
		"neither":   `[{}]`,
		"unknown":   `[{"field":"missing"}]`,
	} {
		t.Run(name, func(t *testing.T) {
			row := replaceJSONArray(validLocalTool, `"argv":[`, `],"stdin"`, argv)
			if _, err := LoadSchemaJSONL(strings.NewReader(row)); err == nil {
				t.Fatalf("loader accepted argv %s", argv)
			}
		})
	}
}

func TestLocalToolTemplateReferencesRequireInputs(t *testing.T) {
	optionalArgv := strings.Replace(validLocalTool, `,"required":true`, "", 1)
	optionalStdin := strings.Replace(validLocalTool, `,"required":true`, "", 1)
	optionalStdin = replaceJSONArray(optionalStdin, `"argv":[`, `],"stdin"`, `[{"literal":"--stdin"}]`)
	optionalStdin = strings.Replace(optionalStdin, `"stdin":{"mode":"none","max_bytes":0}`, `"stdin":{"mode":"field","field":"value","max_bytes":1024}`, 1)
	for name, row := range map[string]string{"argv": optionalArgv, "stdin": optionalStdin} {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadSchemaJSONL(strings.NewReader(row)); err == nil {
				t.Fatalf("loader accepted optional input referenced by %s", name)
			}
		})
	}
}

func TestLoadLocalToolRejectsUnboundedLimits(t *testing.T) {
	for name, replacement := range map[string]string{
		"timeout-zero": `"timeout_ms":0`,
		"timeout-high": `"timeout_ms":300001`,
		"stdout-zero":  `"stdout_bytes":0`,
		"stderr-high":  `"stderr_bytes":16777217`,
	} {
		t.Run(name, func(t *testing.T) {
			row := validLocalTool
			switch {
			case strings.Contains(replacement, "timeout_ms"):
				row = strings.Replace(row, `"timeout_ms":1000`, replacement, 1)
			case strings.Contains(replacement, "stdout_bytes"):
				row = strings.Replace(row, `"stdout_bytes":4096`, replacement, 1)
			default:
				row = strings.Replace(row, `"stderr_bytes":4096`, replacement, 1)
			}
			if _, err := LoadSchemaJSONL(strings.NewReader(row)); err == nil {
				t.Fatalf("loader accepted limits: %s", replacement)
			}
		})
	}
}

func TestLocalToolFirstChildRejectsUnimplementedLifecycleAndApproval(t *testing.T) {
	for name, row := range map[string]string{
		"continuable": strings.Replace(validLocalTool, `"lifecycle":"one-shot"`, `"lifecycle":"continuable"`, 1),
		"no-approval": strings.Replace(validLocalTool, `"approval":"explicit"`, `"approval":"none"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadSchemaJSONL(strings.NewReader(row)); err == nil {
				t.Fatalf("loader accepted unsupported first-child semantics: %s", row)
			}
		})
	}
}

func TestLocalToolLoadsRunViewPoliciesAndFinalSelectorStrictly(t *testing.T) {
	row := strings.Replace(validLocalTool, `"output":{"format":"text"}`, `"output":{"format":"jsonl","final":{"source":"stdout","path":["result"]}},"run_view":{"policy":"required","tool_id":"herdr","tool_version":"1","action_id":"run-view.open"}`, 1)
	viewer := `{"kind":"hq.local-tool.v1","tool_id":"herdr","tool_version":"1","binding_ref":"local-tool.herdr","binding_contract_version":"2","actions":[{"action_id":"run-view.open","inputs":[{"name":"view_id","type":"string","required":true},{"name":"run_id","type":"string","required":true},{"name":"events_path","type":"string","required":true}],"argv":[{"literal":"agent"}],"stdin":{"mode":"none","max_bytes":0},"limits":{"timeout_ms":1000,"stdout_bytes":4096,"stderr_bytes":4096},"output":{"format":"json"},"native_refs":{"session":{"source":"stdout","path":["result","agent","terminal_id"]}},"lifecycle":"one-shot","risk":"low","approval":"explicit"}]}`
	for _, policy := range []string{"required", "optional"} {
		t.Run(policy, func(t *testing.T) {
			candidate := strings.Replace(row, `"policy":"required"`, `"policy":"`+policy+`"`, 1)
			world, err := LoadSchemaJSONL(strings.NewReader(candidate + "\n" + viewer))
			if err != nil {
				t.Fatal(err)
			}
			tool, ok := world.LocalTool("dummy", "1")
			if !ok {
				t.Fatal("loaded world lost source local tool")
			}
			action := tool.Actions[0]
			if action.RunView == nil || action.RunView.Policy != policy || action.Output.Final == nil || action.Output.Final.Path[0] != "result" {
				t.Fatalf("action=%+v", action)
			}
		})
	}

	for name, invalid := range map[string]string{
		"unsupported-view": strings.Replace(row, `"policy":"required"`, `"policy":"none"`, 1),
		"text-final":       strings.Replace(row, `"format":"jsonl"`, `"format":"text"`, 1),
		"empty-view-id":    strings.Replace(row, `"tool_id":"herdr"`, `"tool_id":""`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadSchemaJSONL(strings.NewReader(invalid + "\n" + viewer)); err == nil {
				t.Fatalf("loader accepted invalid run-view contract: %s", invalid)
			}
		})
	}
}

func TestLocalToolWorldIsIndependentOfRecordOrder(t *testing.T) {
	first, err := LoadSchemaJSONL(strings.NewReader(validLocalToolCommand + "\n" + secondLocalTool + "\n" + validLocalTool))
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadSchemaJSONL(strings.NewReader(validLocalTool + "\n" + validLocalToolCommand + "\n" + secondLocalTool))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("world depends on JSONL row order:\nfirst=%#v\nsecond=%#v", first, second)
	}
	if got := first.LocalTools[0].ToolID; got != "alpha" {
		t.Fatalf("local tools are not deterministically ordered: %q", got)
	}
}

func TestLocalToolCommandCrossReferenceFailsClosed(t *testing.T) {
	valid, err := LoadSchemaJSONL(strings.NewReader(validLocalToolCommand + "\n" + validLocalTool))
	if err != nil || len(valid.Commands) != 1 {
		t.Fatalf("valid local-tool command: world=%#v err=%v", valid, err)
	}
	for name, command := range map[string]string{
		"tool":    strings.Replace(validLocalToolCommand, `"tool_id":"dummy"`, `"tool_id":"missing"`, 1),
		"version": strings.Replace(validLocalToolCommand, `"tool_version":"1"`, `"tool_version":"2"`, 1),
		"action":  strings.Replace(validLocalToolCommand, `"action_id":"echo"`, `"action_id":"missing"`, 1),
		"bind":    strings.Replace(validLocalToolCommand, `payload.input.value`, `payload.value`, 1),
		"type":    strings.Replace(validLocalToolCommand, `"type":"string"`, `"type":"integer"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadSchemaJSONL(strings.NewReader(command + "\n" + validLocalTool)); err == nil {
				t.Fatalf("loader accepted invalid local-tool command: %s", command)
			}
		})
	}
}

func replaceJSONArray(row, start, end, replacement string) string {
	startIndex := strings.Index(row, start)
	endIndex := strings.Index(row[startIndex+len(start):], end)
	if startIndex < 0 || endIndex < 0 {
		return row
	}
	endIndex += startIndex + len(start)
	return row[:startIndex] + `"argv":` + replacement + row[endIndex+1:]
}

func TestCommandRecallVocabularyLoadsStrictly(t *testing.T) {
	row := `{"kind":"hq.command.v1","name":"herdr.read","aliases":["agent.read"],"keywords":["reviewer"],"description":"read output","instruction":{"op":"run"},"fields":[{"name":"agent","type":"string","required":true,"examples":["reviewer"],"bind":"payload.agent"},{"name":"source","type":"enum","required":true,"enum":["screen"],"default":"screen","materialized_values":["screen"],"bind":"payload.source"},{"name":"lines","type":"integer","required":true,"default":100,"materialized_values":[50,200],"bind":"payload.lines"}],"presets":[{"id":"reviewer-screen","label":"Reviewer screen","values":{"agent":"reviewer","source":"screen","lines":100}}]}`
	world, err := LoadSchemaJSONL(strings.NewReader(row))
	if err != nil {
		t.Fatal(err)
	}
	command := world.Commands[0]
	if len(command.Aliases) != 1 || len(command.Keywords) != 1 || len(command.Presets) != 1 {
		t.Fatalf("command=%#v", command)
	}
	if command.Fields[1].Default == nil || command.Fields[1].Default.Text() != "screen" || len(command.Fields[2].MaterializedValues) != 2 {
		t.Fatalf("fields=%#v", command.Fields)
	}
}

func TestCommandRecallVocabularyFailsClosed(t *testing.T) {
	basePrefix := `{"kind":"hq.command.v1","name":"bad","instruction":{},"fields":[`
	baseSuffix := `]}`
	cases := map[string]string{
		"unknown command member":    `{"kind":"hq.command.v1","name":"bad","alias":["x"],"instruction":{}}`,
		"unknown field member":      basePrefix + `{"name":"x","type":"string","bind":"payload.x","suggestions":["x"]}` + baseSuffix,
		"duplicate alias":           `{"kind":"hq.command.v1","name":"bad","aliases":["x","x"],"instruction":{}}`,
		"wrong typed default":       basePrefix + `{"name":"lines","type":"integer","default":"100","bind":"payload.lines"}` + baseSuffix,
		"enum default outside enum": basePrefix + `{"name":"source","type":"enum","enum":["screen"],"default":"file","bind":"payload.source"}` + baseSuffix,
		"preset missing required":   basePrefix + `{"name":"path","type":"path","required":true,"bind":"payload.path"}` + `],"presets":[{"id":"empty","label":"Empty","values":{}}]}`,
		"preset unknown field":      basePrefix + `{"name":"path","type":"path","required":true,"bind":"payload.path"}` + `],"presets":[{"id":"bad","label":"Bad","values":{"path":"C:\\work","other":"x"}}]}`,
		"non materializable value":  basePrefix + `{"name":"path","type":"path","required":true,"examples":["bad\"path"],"bind":"payload.path"}` + baseSuffix,
	}
	for name, row := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadSchemaJSONL(strings.NewReader(row)); err == nil {
				t.Fatalf("invalid row loaded: %s", row)
			}
		})
	}
}
