package adapter

import (
	"strings"
	"testing"
)

func TestProviderDescriptorValidatesAndComparesOrderedDependencies(t *testing.T) {
	digestA := "sha256:" + strings.Repeat("a", 64)
	digestB := "sha256:" + strings.Repeat("b", 64)
	descriptor := ProviderDescriptor{
		CapabilityID: "local-tool:demo@1/start", ProviderID: "local-tool.demo", ContractVersion: "2",
		DeploymentID: "app.demo@1:" + digestA, ProviderKind: "executable", IntegrityDigest: digestA,
		ConfigurationDigest: digestB,
		Dependencies: []ProviderDependencyDescriptor{{
			Name: "child", ProviderID: "local-tool.child", ContractVersion: "1",
			DeploymentID: "app.child@1:" + digestB, ProviderKind: "executable", IntegrityDigest: digestB,
		}},
	}
	if err := descriptor.Validate(); err != nil {
		t.Fatal(err)
	}
	copy := descriptor
	copy.Dependencies = append([]ProviderDependencyDescriptor(nil), descriptor.Dependencies...)
	if !descriptor.Equal(copy) {
		t.Fatal("equal provider dependency evidence did not compare equal")
	}
	copy.Dependencies[0].IntegrityDigest = digestA
	if descriptor.Equal(copy) {
		t.Fatal("dependency digest drift compared equal")
	}
	copy = descriptor
	copy.ConfigurationDigest = digestA
	if descriptor.Equal(copy) {
		t.Fatal("primary configuration drift compared equal")
	}
}

func TestProviderDescriptorRejectsDuplicateOrUnsortedDependencies(t *testing.T) {
	digest := "sha256:" + strings.Repeat("c", 64)
	base := ProviderDescriptor{
		CapabilityID: "cap", ProviderID: "provider", ContractVersion: "1", DeploymentID: "deployment",
		ProviderKind: "executable", IntegrityDigest: digest,
	}
	dependency := func(name string) ProviderDependencyDescriptor {
		return ProviderDependencyDescriptor{Name: name, ProviderID: "binding." + name, ContractVersion: "1", DeploymentID: "deployment." + name, ProviderKind: "executable", IntegrityDigest: digest}
	}
	for name, dependencies := range map[string][]ProviderDependencyDescriptor{
		"duplicate": {dependency("same"), dependency("same")},
		"unsorted":  {dependency("z"), dependency("a")},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := base
			candidate.Dependencies = dependencies
			if err := candidate.Validate(); err == nil {
				t.Fatalf("accepted dependencies=%+v", dependencies)
			}
		})
	}
}
