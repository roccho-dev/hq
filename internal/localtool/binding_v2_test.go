package localtool

import (
	"context"
	"strings"
	"testing"

	"hq/internal/worker/adapter"
)

func TestBindingContractV2UsesOnlyVerifiedOrderedEnvironment(t *testing.T) {
	t.Setenv("HQ_LOCAL_TOOL_POISON", "ambient-must-not-win")
	executable, registryPath := executableBindingFixture(t, "local-tool.dummy", "1", "app.dummy")
	environment := []VerifiedBindingEnvironment{{Name: "HQ_LOCAL_TOOL_POISON", Value: "binding-owned"}}
	binding := verifiedBindingForTest(t, executable, "local-tool.dummy", "app.dummy", "2")
	binding.Environment = environment
	binding.ConfigurationDigest = BindingEnvironmentDigest(environment)
	writeRegistry(t, registryPath, VerifiedBindings{Schema: VerifiedBindingsSchema, Entries: []VerifiedBinding{binding}})

	loaded, err := LoadVerifiedBinding(registryPath, binding.BindingRef, "2")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ConfigurationDigest != binding.ConfigurationDigest || len(loaded.EnvironmentStrings()) != 1 || loaded.EnvironmentStrings()[0] != "HQ_LOCAL_TOOL_POISON=binding-owned" {
		t.Fatalf("loaded binding=%+v env=%v", loaded, loaded.EnvironmentStrings())
	}
	tool := helperTool("env", "text")
	tool.BindingContractVersion = "2"
	prepared := prepare(t, tool, registryPath, nil)
	if prepared.Provider == nil || prepared.Provider.ConfigurationDigest != binding.ConfigurationDigest {
		t.Fatalf("provider=%+v", prepared.Provider)
	}
	completion, err := prepared.Adapter.Run(context.Background(), requestFor(tool, nil), func(adapter.Output) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if completion.FinalText != "binding-owned" {
		t.Fatalf("ambient environment leaked or binding environment missing: %q", completion.FinalText)
	}
}

func TestBindingConfigurationDigestRejectsAllZeroAndMalformedEnvironment(t *testing.T) {
	executable, registryPath := executableBindingFixture(t, "local-tool.dummy", "1", "app.dummy")
	validEnvironment := []VerifiedBindingEnvironment{{Name: "APPDATA", Value: "/exact/profile"}}
	valid := verifiedBindingForTest(t, executable, "local-tool.dummy", "app.dummy", "2")
	valid.Environment = validEnvironment
	valid.ConfigurationDigest = BindingEnvironmentDigest(validEnvironment)

	tests := map[string]func(*VerifiedBinding){
		"all-zero digest": func(binding *VerifiedBinding) {
			binding.ConfigurationDigest = "sha256:" + strings.Repeat("0", 64)
		},
		"missing environment": func(binding *VerifiedBinding) {
			binding.Environment = nil
			binding.ConfigurationDigest = ""
		},
		"contract one carries environment": func(binding *VerifiedBinding) {
			binding.ContractVersion = "1"
		},
		"unsorted environment": func(binding *VerifiedBinding) {
			binding.Environment = []VerifiedBindingEnvironment{{Name: "ZED", Value: "z"}, {Name: "APPDATA", Value: "a"}}
			binding.ConfigurationDigest = BindingEnvironmentDigest(binding.Environment)
		},
		"duplicate environment": func(binding *VerifiedBinding) {
			binding.Environment = []VerifiedBindingEnvironment{{Name: "APPDATA", Value: "a"}, {Name: "APPDATA", Value: "b"}}
			binding.ConfigurationDigest = BindingEnvironmentDigest(binding.Environment)
		},
		"lowercase environment": func(binding *VerifiedBinding) {
			binding.Environment = []VerifiedBindingEnvironment{{Name: "AppData", Value: "a"}}
			binding.ConfigurationDigest = BindingEnvironmentDigest(binding.Environment)
		},
		"nul environment": func(binding *VerifiedBinding) {
			binding.Environment = []VerifiedBindingEnvironment{{Name: "APPDATA", Value: "a\x00b"}}
			binding.ConfigurationDigest = BindingEnvironmentDigest(binding.Environment)
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			candidate := valid
			candidate.Environment = append([]VerifiedBindingEnvironment(nil), valid.Environment...)
			mutate(&candidate)
			writeRegistry(t, registryPath, VerifiedBindings{Schema: VerifiedBindingsSchema, Entries: []VerifiedBinding{candidate}})
			_, err := LoadVerifiedBinding(registryPath, candidate.BindingRef, candidate.ContractVersion)
			assertFailureCode(t, err, "binding_registry_invalid")
		})
	}
}

func TestBindingContractV1RemainsEmptyAndCompatible(t *testing.T) {
	_, registryPath := executableBindingFixture(t, "local-tool.dummy", "1", "app.dummy")
	binding, err := LoadVerifiedBinding(registryPath, "local-tool.dummy", "1")
	if err != nil {
		t.Fatal(err)
	}
	if binding.ConfigurationDigest != "" || len(binding.Environment) != 0 || len(binding.EnvironmentStrings()) != 0 {
		t.Fatalf("contract v1 binding=%+v", binding)
	}
}

func verifiedBindingForTest(t *testing.T, executable, bindingRef, resourceID, contractVersion string) VerifiedBinding {
	t.Helper()
	digest := sha256File(t, executable)
	return VerifiedBinding{
		BindingRef: bindingRef, ResourceID: resourceID, ContractVersion: contractVersion, Executable: executable,
		MaterialDigest: digest, DeploymentID: resourceID + "@1:" + digest,
		DeclarationEventID: bindingRef + "-declared", SelectionEventID: bindingRef + "-selected",
	}
}
