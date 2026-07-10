package capability

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadVerifiesExactDeploymentAndBytes(t *testing.T) {
	root := t.TempDir()
	provider := filepath.Join(root, "provider.bin")
	content := []byte("exact-provider-bytes")
	if err := os.WriteFile(provider, content, 0o700); err != nil { t.Fatal(err) }
	hash := sha256.Sum256(content)
	registry := Registry{Kind: RegistryKind, DeploymentID: "dep-1", Bindings: []Binding{{
		Kind: BindingKind, CapabilityID: HostOpenCapability, ProviderID: "test-provider",
		ContractVersion: HostOpenContract, DeploymentID: "dep-1", ProviderKind: "executable",
		ExecutablePath: provider, IntegrityDigest: "sha256:" + hex.EncodeToString(hash[:]), VerificationStatus: "verified",
	}}}
	path := filepath.Join(root, "capabilities.json")
	encoded, _ := json.Marshal(registry)
	if err := os.WriteFile(path, encoded, 0o600); err != nil { t.Fatal(err) }
	binding, err := Load(path, "dep-1", HostOpenCapability)
	if err != nil { t.Fatal(err) }
	if binding.ProviderID != "test-provider" { t.Fatalf("binding=%+v", binding) }
	if err := os.WriteFile(provider, []byte("tampered"), 0o700); err != nil { t.Fatal(err) }
	if _, err := Load(path, "dep-1", HostOpenCapability); err == nil { t.Fatal("tampered provider must fail") }
}

func TestLoadRejectsWrongDeploymentAndDuplicateCapability(t *testing.T) {
	root := t.TempDir()
	provider := filepath.Join(root, "provider.bin")
	content := []byte("provider")
	if err := os.WriteFile(provider, content, 0o700); err != nil { t.Fatal(err) }
	hash := sha256.Sum256(content)
	binding := Binding{Kind: BindingKind, CapabilityID: HostOpenCapability, ProviderID: "p", ContractVersion: HostOpenContract, DeploymentID: "dep", ProviderKind: "executable", ExecutablePath: provider, IntegrityDigest: "sha256:" + hex.EncodeToString(hash[:]), VerificationStatus: "verified"}
	registry := Registry{Kind: RegistryKind, DeploymentID: "dep", Bindings: []Binding{binding, binding}}
	path := filepath.Join(root, "capabilities.json")
	encoded, _ := json.Marshal(registry)
	if err := os.WriteFile(path, encoded, 0o600); err != nil { t.Fatal(err) }
	if _, err := Load(path, "other", HostOpenCapability); err == nil { t.Fatal("deployment mismatch must fail") }
	if _, err := Load(path, "dep", HostOpenCapability); err == nil { t.Fatal("duplicate capability must fail") }
}
