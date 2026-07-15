package current

import (
	"strings"
	"testing"
)

const validInvocableResource = `{"kind":"hq.local-tool.v1","tool_id":"aws","tool_version":"2.35.11","binding_ref":"local-tool.aws-restricted","binding_contract_version":"1","invocation":{"policy_version":"aws-restricted.v1","denied_options":["--profile","--endpoint-url"],"denied_argument_prefixes":["file://","@"],"max_argv":32,"max_arg_bytes":4096,"limits":{"timeout_ms":60000,"stdout_bytes":1048576,"stderr_bytes":1048576}}}`

func TestInvocableResourceLoadsWithoutFiniteActions(t *testing.T) {
	world, err := LoadSchemaJSONL(strings.NewReader(validInvocableResource))
	if err != nil {
		t.Fatal(err)
	}
	tool, ok := world.LocalTool("aws", "2.35.11")
	if !ok || tool.Invocation == nil || tool.Invocation.PolicyVersion != "aws-restricted.v1" || len(tool.Actions) != 0 {
		t.Fatalf("tool=%+v ok=%v", tool, ok)
	}
}

func TestLocalToolRequiresActionOrInvocation(t *testing.T) {
	row := `{"kind":"hq.local-tool.v1","tool_id":"empty","tool_version":"1","binding_ref":"local-tool.empty","binding_contract_version":"1"}`
	if _, err := LoadSchemaJSONL(strings.NewReader(row)); err == nil {
		t.Fatal("empty local tool loaded")
	}
}

func TestInvocableResourcePolicyFailsClosed(t *testing.T) {
	for name, row := range map[string]string{
		"zero argv": strings.Replace(validInvocableResource, `"max_argv":32`, `"max_argv":0`, 1),
		"oversize bytes": strings.Replace(validInvocableResource, `"max_arg_bytes":4096`, `"max_arg_bytes":65537`, 1),
		"unbounded timeout": strings.Replace(validInvocableResource, `"timeout_ms":60000`, `"timeout_ms":300001`, 1),
		"invalid option": strings.Replace(validInvocableResource, `"--profile"`, `"profile"`, 1),
		"duplicate option": strings.Replace(validInvocableResource, `"--profile","--endpoint-url"`, `"--profile","--profile"`, 1),
		"unknown member": strings.Replace(validInvocableResource, `"policy_version":"aws-restricted.v1"`, `"policy_version":"aws-restricted.v1","allow_shell":true`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadSchemaJSONL(strings.NewReader(row)); err == nil {
				t.Fatalf("invalid invocation policy loaded: %s", row)
			}
		})
	}
}
