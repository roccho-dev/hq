package localtool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"hq/internal/core"
	"hq/internal/worker/adapter"
)

func TestPreparerExercisesExactDependencyAndReverifiesBeforeEffect(t *testing.T) {
	primary, dependency, registryPath := dependencyBindingFixture(t)
	tool := dependencyTool(t, true)
	prepared := prepare(t, tool, registryPath, nil)
	if prepared.Provider == nil || len(prepared.Provider.Dependencies) != 1 {
		t.Fatalf("provider=%+v", prepared.Provider)
	}
	evidence := prepared.Provider.Dependencies[0]
	if evidence.Name != "child" || evidence.ProviderID != "local-tool.child" || evidence.IntegrityDigest != sha256File(t, dependency) {
		t.Fatalf("dependency evidence=%+v", evidence)
	}
	var stdout string
	completion, err := prepared.Adapter.Run(context.Background(), requestFor(tool, nil), func(output adapter.Output) error {
		if output.Kind == adapter.OutputStdout {
			stdout += output.Message
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if stdout != dependency || completion.FinalText != dependency {
		t.Fatalf("binding_executable was not exact: stdout=%q completion=%q dependency=%q primary=%q", stdout, completion.FinalText, dependency, primary)
	}

	if err := os.WriteFile(dependency, []byte("tampered dependency"), 0o700); err != nil {
		t.Fatal(err)
	}
	emitted := false
	_, err = prepared.Adapter.Run(context.Background(), requestFor(tool, nil), func(adapter.Output) error {
		emitted = true
		return nil
	})
	assertFailureCode(t, err, "binding_digest_mismatch")
	if emitted {
		t.Fatal("dependency drift reached process output")
	}
}

func TestPreparerDoesNotResolveDeclaredButUnusedDependency(t *testing.T) {
	_, _, registryPath := dependencyBindingFixture(t)
	tool := dependencyTool(t, false)
	tool.Bindings[0].BindingRef = "local-tool.not-present"
	prepared := prepare(t, tool, registryPath, nil)
	if prepared.Provider == nil || len(prepared.Provider.Dependencies) != 0 {
		t.Fatalf("unused dependency appeared in provider evidence: %+v", prepared.Provider)
	}
	completion, err := prepared.Adapter.Run(context.Background(), requestFor(tool, nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	if completion.FinalText != "safe" {
		t.Fatalf("completion=%+v", completion)
	}
}

func dependencyBindingFixture(t *testing.T) (string, string, string) {
	t.Helper()
	source, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	primary := filepath.Join(root, "primary"+filepath.Ext(source))
	dependency := filepath.Join(root, "dependency"+filepath.Ext(source))
	for _, path := range []string{primary, dependency} {
		if err := os.WriteFile(path, data, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	primaryBinding := verifiedBindingForTest(t, primary, "local-tool.primary", "app.primary", "1")
	dependencyBinding := verifiedBindingForTest(t, dependency, "local-tool.child", "app.child", "1")
	registryPath := filepath.Join(root, "verified-executable-bindings.json")
	writeRegistry(t, registryPath, VerifiedBindings{Schema: VerifiedBindingsSchema, Entries: []VerifiedBinding{primaryBinding, dependencyBinding}})
	return primary, dependency, registryPath
}

func dependencyTool(t *testing.T, useDependency bool) core.LocalToolDefinition {
	t.Helper()
	argument := `{"literal":"safe"}`
	if useDependency {
		argument = `{"binding_executable":"child"}`
	}
	row := `{"kind":"hq.local-tool.v1","tool_id":"dummy","tool_version":"1","binding_ref":"local-tool.primary","binding_contract_version":"1","bindings":[{"name":"child","binding_ref":"local-tool.child","binding_contract_version":"1"}],"actions":[{"action_id":"proof","argv":[{"literal":"-test.run=^TestLocalToolHelperProcess$"},{"literal":"--"},{"literal":"echo"},` + argument + `],"stdin":{"mode":"none","max_bytes":0},"limits":{"timeout_ms":2000,"stdout_bytes":4096,"stderr_bytes":4096},"output":{"format":"text"},"lifecycle":"one-shot","risk":"low","approval":"explicit"}]}`
	var tool core.LocalToolDefinition
	if err := json.Unmarshal([]byte(row), &tool); err != nil {
		t.Fatal(err)
	}
	return tool
}
