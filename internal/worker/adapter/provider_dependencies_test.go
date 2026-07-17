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
		Dependencies: []ProviderDependencyDescriptor{
			{Name: "second", ProviderID: "local-tool.second", ContractVersion: "1", DeploymentID: "app.second@1:" + digestB, ProviderKind: "executable", IntegrityDigest: digestB},
			{Name: "first", ProviderID: "local-tool.first", ContractVersion: "1", DeploymentID: "app.first@1:" + digestA, ProviderKind: "executable", IntegrityDigest: digestA},
		},
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
	copy.Dependencies = []ProviderDependencyDescriptor{descriptor.Dependencies[1], descriptor.Dependencies[0]}
	if descriptor.Equal(copy) {
		t.Fatal("dependency reordering compared equal")
	}
	copy = descriptor
	copy.ConfigurationDigest = digestA
	if descriptor.Equal(copy) {
		t.Fatal("primary configuration drift compared equal")
	}
}

func TestProviderDescriptorRejectsDuplicateDependencyNames(t *testing.T) {
	digest := "sha256:" + strings.Repeat("c", 64)
	dependency := func(provider string) ProviderDependencyDescriptor {
		return ProviderDependencyDescriptor{Name: "same", ProviderID: provider, ContractVersion: "1", DeploymentID: "deployment." + provider, ProviderKind: "executable", IntegrityDigest: digest}
	}
	descriptor := ProviderDescriptor{
		CapabilityID: "cap", ProviderID: "provider", ContractVersion: "1", DeploymentID: "deployment",
		ProviderKind: "executable", IntegrityDigest: digest,
		Dependencies: []ProviderDependencyDescriptor{dependency("binding.a"), dependency("binding.b")},
	}
	if err := descriptor.Validate(); err == nil {
		t.Fatalf("accepted duplicate dependencies=%+v", descriptor.Dependencies)
	}
}
