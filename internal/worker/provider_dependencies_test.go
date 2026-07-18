package worker

import (
	"strings"
	"testing"

	"hq/internal/worker/adapter"
)

func TestRecoveryProviderMatchIncludesConfigurationAndDependencies(t *testing.T) {
	digestA := "sha256:" + strings.Repeat("a", 64)
	digestB := "sha256:" + strings.Repeat("b", 64)
	descriptor := adapter.ProviderDescriptor{
		CapabilityID: "local-tool:demo@1/start", ProviderID: "local-tool.demo", ContractVersion: "2",
		DeploymentID: "app.demo@1:" + digestA, ProviderKind: "executable", IntegrityDigest: digestA,
		ConfigurationDigest: digestB, IdempotencyContract: "provider-key.v1",
		Dependencies: []adapter.ProviderDependencyDescriptor{{
			Name: "child", ProviderID: "local-tool.child", ContractVersion: "1",
			DeploymentID: "app.child@1:" + digestB, ProviderKind: "executable", IntegrityDigest: digestB,
		}},
	}
	prior := providerEvidence(descriptor, "key-1")
	if prior == nil || !providerMatches(*prior, descriptor) {
		t.Fatalf("exact provider did not match: prior=%+v descriptor=%+v", prior, descriptor)
	}

	configurationDrift := descriptor
	configurationDrift.ConfigurationDigest = digestA
	if providerMatches(*prior, configurationDrift) {
		t.Fatal("primary configuration drift matched recovery evidence")
	}

	dependencyDrift := descriptor
	dependencyDrift.Dependencies = append([]adapter.ProviderDependencyDescriptor(nil), descriptor.Dependencies...)
	dependencyDrift.Dependencies[0].DeploymentID = "app.child@2:" + digestB
	if providerMatches(*prior, dependencyDrift) {
		t.Fatal("dependency deployment drift matched recovery evidence")
	}

	missingDependency := descriptor
	missingDependency.Dependencies = nil
	if providerMatches(*prior, missingDependency) {
		t.Fatal("missing dependency matched recovery evidence")
	}
}

func TestProviderEvidenceValidationRejectsMalformedDependency(t *testing.T) {
	digest := "sha256:" + strings.Repeat("d", 64)
	evidence := ProviderEvidence{
		CapabilityID: "cap", ProviderID: "provider", ContractVersion: "1", DeploymentID: "deployment",
		ProviderKind: "executable", IntegrityDigest: digest,
		Dependencies: []ProviderDependencyEvidence{{
			Name: "child", ProviderID: "binding.child", ContractVersion: "1", DeploymentID: "deployment.child",
			ProviderKind: "executable", IntegrityDigest: digest,
		}},
	}
	if err := evidence.Validate(); err != nil {
		t.Fatal(err)
	}
	evidence.Dependencies[0].Name = "bad name"
	if err := evidence.Validate(); err == nil {
		t.Fatal("accepted malformed dependency evidence")
	}
}
