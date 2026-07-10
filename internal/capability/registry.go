// Package capability validates envs-generated provider bindings immediately
// before a worker makes them executable. It does not select providers or use
// PATH; it only admits exact deployment-bound identities.
package capability

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	RegistryKind = "hq.capabilityRegistry.v1"
	BindingKind = "hq.capabilityBinding.v1"
	HostOpenCapability = "host.open"
	HostOpenContract = "host.open.v1"
)

type Registry struct {
	Kind         string    `json:"kind"`
	DeploymentID string    `json:"deployment_id"`
	Bindings     []Binding `json:"bindings"`
}

type Binding struct {
	Kind                string `json:"kind"`
	CapabilityID        string `json:"capability_id"`
	ProviderID          string `json:"provider_id"`
	ContractVersion     string `json:"contract_version"`
	DeploymentID        string `json:"deployment_id"`
	ProviderKind        string `json:"provider_kind"`
	ExecutablePath      string `json:"executable_path"`
	IntegrityDigest     string `json:"integrity_digest"`
	VerificationStatus  string `json:"verification_status"`
	IdempotencyContract string `json:"idempotency_contract,omitempty"`
}

func Load(path, expectedDeployment, capabilityID string) (Binding, error) {
	if !filepath.IsAbs(path) { return Binding{}, errors.New("capability registry path must be absolute") }
	file, err := os.Open(path)
	if err != nil { return Binding{}, fmt.Errorf("open capability registry: %w", err) }
	defer file.Close()
	registry, err := Decode(file)
	if err != nil { return Binding{}, err }
	if registry.Kind != RegistryKind { return Binding{}, fmt.Errorf("registry kind must be %q", RegistryKind) }
	if registry.DeploymentID != expectedDeployment || strings.TrimSpace(expectedDeployment) == "" {
		return Binding{}, errors.New("capability registry deployment identity mismatch")
	}
	var selected *Binding
	seen := map[string]struct{}{}
	for i := range registry.Bindings {
		binding := &registry.Bindings[i]
		if _, exists := seen[binding.CapabilityID]; exists { return Binding{}, fmt.Errorf("duplicate capability binding %q", binding.CapabilityID) }
		seen[binding.CapabilityID] = struct{}{}
		if err := binding.Validate(registry.DeploymentID); err != nil { return Binding{}, fmt.Errorf("binding %d: %w", i+1, err) }
		if binding.CapabilityID == capabilityID { selected = binding }
	}
	if selected == nil { return Binding{}, fmt.Errorf("capability %q is unavailable", capabilityID) }
	if err := selected.VerifyExecutable(); err != nil { return Binding{}, err }
	return *selected, nil
}

func Decode(r io.Reader) (Registry, error) {
	data, err := io.ReadAll(io.LimitReader(r, 4<<20))
	if err != nil { return Registry{}, err }
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var registry Registry
	if err := decoder.Decode(&registry); err != nil { return Registry{}, err }
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil { return Registry{}, errors.New("capability registry contains multiple JSON values") }
		return Registry{}, err
	}
	return registry, nil
}

func (b Binding) Validate(expectedDeployment string) error {
	if b.Kind != BindingKind { return fmt.Errorf("kind must be %q", BindingKind) }
	if b.CapabilityID != HostOpenCapability { return fmt.Errorf("unsupported capability %q", b.CapabilityID) }
	if b.ContractVersion != HostOpenContract { return fmt.Errorf("contract_version must be %q", HostOpenContract) }
	if b.DeploymentID != expectedDeployment { return errors.New("binding deployment identity mismatch") }
	if strings.TrimSpace(b.ProviderID) == "" { return errors.New("provider_id is required") }
	if b.ProviderKind != "executable" { return errors.New("provider_kind must be executable in v1") }
	if !filepath.IsAbs(b.ExecutablePath) { return errors.New("executable_path must be absolute") }
	if b.VerificationStatus != "verified" { return errors.New("verification_status must be verified") }
	if err := validateDigest(b.IntegrityDigest); err != nil { return err }
	return nil
}

func (b Binding) VerifyExecutable() error {
	info, err := os.Stat(b.ExecutablePath)
	if err != nil { return fmt.Errorf("stat provider executable: %w", err) }
	if !info.Mode().IsRegular() { return errors.New("provider executable is not a regular file") }
	file, err := os.Open(b.ExecutablePath)
	if err != nil { return fmt.Errorf("open provider executable: %w", err) }
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil { return fmt.Errorf("hash provider executable: %w", err) }
	actual := "sha256:" + hex.EncodeToString(hash.Sum(nil))
	if actual != b.IntegrityDigest { return fmt.Errorf("provider integrity mismatch: expected %s, got %s", b.IntegrityDigest, actual) }
	return nil
}

func validateDigest(value string) error {
	hexValue := strings.TrimPrefix(value, "sha256:")
	if !strings.HasPrefix(value, "sha256:") || len(hexValue) != sha256.Size*2 || hexValue != strings.ToLower(hexValue) {
		return errors.New("integrity_digest must be sha256:<64 lowercase hex chars>")
	}
	if _, err := hex.DecodeString(hexValue); err != nil { return errors.New("integrity_digest contains invalid hex") }
	return nil
}
