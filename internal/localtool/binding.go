package localtool

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

const VerifiedBindingsSchema = "envctl.verified-executable-bindings.v1"
const bindingEnvironmentDigestDomain = "envctl.verifiedExecutableBinding.environment.v1\x00"

type VerifiedBindings struct {
	Schema  string            `json:"schema"`
	Entries []VerifiedBinding `json:"entries"`
}

type VerifiedBinding struct {
	BindingRef          string                       `json:"bindingRef"`
	ResourceID          string                       `json:"resourceId"`
	ContractVersion     string                       `json:"contractVersion"`
	Executable          string                       `json:"executable"`
	MaterialDigest      string                       `json:"materialDigest"`
	Environment         []VerifiedBindingEnvironment `json:"environment,omitempty"`
	ConfigurationDigest string                       `json:"configurationDigest,omitempty"`
	DeploymentID        string                       `json:"deploymentId"`
	DeclarationEventID  string                       `json:"declarationEventId"`
	SelectionEventID    string                       `json:"selectionEventId"`
}

type VerifiedBindingEnvironment struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

func LoadVerifiedBinding(path, bindingRef, contractVersion string) (VerifiedBinding, error) {
	if strings.TrimSpace(path) == "" || !filepath.IsAbs(path) {
		return VerifiedBinding{}, blocked("binding_registry_path_invalid", "verified executable bindings path must be absolute")
	}
	f, err := os.Open(path)
	if err != nil {
		return VerifiedBinding{}, blocked("binding_registry_unavailable", fmt.Sprintf("open verified executable bindings: %v", err))
	}
	defer f.Close()
	registry, err := decodeVerifiedBindings(f)
	if err != nil {
		return VerifiedBinding{}, blocked("binding_registry_invalid", err.Error())
	}
	if registry.Schema != VerifiedBindingsSchema {
		return VerifiedBinding{}, blocked("binding_registry_schema_mismatch", fmt.Sprintf("registry schema must be %q", VerifiedBindingsSchema))
	}
	seen := map[string]struct{}{}
	provenanceEvents := map[string]struct{}{}
	var selected *VerifiedBinding
	for i := range registry.Entries {
		entry := registry.Entries[i]
		if _, exists := seen[entry.BindingRef]; exists {
			return VerifiedBinding{}, blocked("binding_duplicate", fmt.Sprintf("duplicate executable binding %q", entry.BindingRef))
		}
		seen[entry.BindingRef] = struct{}{}
		for _, eventID := range []string{entry.DeclarationEventID, entry.SelectionEventID} {
			if _, exists := provenanceEvents[eventID]; exists {
				return VerifiedBinding{}, blocked("binding_provenance_duplicate", fmt.Sprintf("duplicate binding provenance event %q", eventID))
			}
			provenanceEvents[eventID] = struct{}{}
		}
		if err := entry.validate(); err != nil {
			return VerifiedBinding{}, blocked("binding_registry_invalid", fmt.Sprintf("binding %d: %v", i+1, err))
		}
		if entry.BindingRef == bindingRef {
			copy := entry
			copy.Environment = append([]VerifiedBindingEnvironment(nil), entry.Environment...)
			selected = &copy
		}
	}
	if selected == nil {
		return VerifiedBinding{}, blocked("binding_unavailable", fmt.Sprintf("verified executable binding %q is unavailable", bindingRef))
	}
	if selected.ContractVersion != contractVersion {
		return VerifiedBinding{}, blocked("binding_contract_mismatch", fmt.Sprintf("binding %q contract is %q, expected %q", bindingRef, selected.ContractVersion, contractVersion))
	}
	if err := selected.VerifyExecutable(); err != nil {
		return VerifiedBinding{}, err
	}
	return *selected, nil
}

func decodeVerifiedBindings(r io.Reader) (VerifiedBindings, error) {
	const maximumRegistryBytes = 4 << 20
	data, err := io.ReadAll(io.LimitReader(r, maximumRegistryBytes+1))
	if err != nil {
		return VerifiedBindings{}, err
	}
	if len(data) > maximumRegistryBytes {
		return VerifiedBindings{}, errors.New("verified executable bindings exceed 4 MiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var registry VerifiedBindings
	if err := decoder.Decode(&registry); err != nil {
		return VerifiedBindings{}, err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return VerifiedBindings{}, errors.New("verified executable bindings contain multiple JSON values")
		}
		return VerifiedBindings{}, err
	}
	return registry, nil
}

func (b VerifiedBinding) validate() error {
	for field, value := range map[string]string{
		"bindingRef": b.BindingRef, "resourceId": b.ResourceID, "contractVersion": b.ContractVersion,
		"deploymentId": b.DeploymentID, "declarationEventId": b.DeclarationEventID, "selectionEventId": b.SelectionEventID,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", field)
		}
	}
	if !filepath.IsAbs(b.Executable) || filepath.Clean(b.Executable) != b.Executable {
		return errors.New("executable must be a clean absolute immutable path")
	}
	if err := validateDigest(b.MaterialDigest); err != nil {
		return err
	}
	if !strings.HasPrefix(b.DeploymentID, b.ResourceID+"@") || !strings.HasSuffix(b.DeploymentID, ":"+b.MaterialDigest) {
		return errors.New("deploymentId must bind resourceId and materialDigest")
	}
	if err := b.validateEnvironment(); err != nil {
		return err
	}
	return nil
}

func (b VerifiedBinding) validateEnvironment() error {
	switch b.ContractVersion {
	case "1":
		if len(b.Environment) != 0 || b.ConfigurationDigest != "" {
			return errors.New("binding contract 1 requires empty environment and configurationDigest")
		}
		return nil
	case "2":
		if len(b.Environment) == 0 {
			return errors.New("binding contract 2 requires a finite environment")
		}
	default:
		return fmt.Errorf("unsupported binding contract version %q", b.ContractVersion)
	}
	if len(b.Environment) > 64 {
		return errors.New("binding environment exceeds 64 entries")
	}
	previous := ""
	totalBytes := 0
	for index, entry := range b.Environment {
		if !validEnvironmentName(entry.Name) {
			return fmt.Errorf("binding environment %d name is invalid", index+1)
		}
		if previous != "" && entry.Name <= previous {
			return errors.New("binding environment names must be unique and sorted")
		}
		if strings.ContainsRune(entry.Value, '\x00') {
			return fmt.Errorf("binding environment %q contains NUL", entry.Name)
		}
		previous = entry.Name
		totalBytes += len(entry.Name) + len(entry.Value) + 2
	}
	if totalBytes > 64<<10 {
		return errors.New("binding environment exceeds 64 KiB")
	}
	if err := validateConfigurationDigest(b.ConfigurationDigest); err != nil {
		return err
	}
	expected := BindingEnvironmentDigest(b.Environment)
	if b.ConfigurationDigest != expected {
		return fmt.Errorf("configurationDigest does not match environment: expected %s, got %s", expected, b.ConfigurationDigest)
	}
	return nil
}

func validEnvironmentName(value string) bool {
	if value == "" {
		return false
	}
	for index, character := range value {
		if character == '_' || character >= 'A' && character <= 'Z' || index > 0 && character >= '0' && character <= '9' {
			continue
		}
		return false
	}
	return true
}

func BindingEnvironmentDigest(environment []VerifiedBindingEnvironment) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte(bindingEnvironmentDigestDomain))
	for _, entry := range environment {
		_, _ = hash.Write([]byte(entry.Name))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(entry.Value))
		_, _ = hash.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}

func (b VerifiedBinding) EnvironmentStrings() []string {
	if len(b.Environment) == 0 {
		return []string{}
	}
	result := make([]string, 0, len(b.Environment))
	for _, entry := range b.Environment {
		result = append(result, entry.Name+"="+entry.Value)
	}
	return result
}

func (b VerifiedBinding) VerifyExecutable() error {
	info, err := os.Lstat(b.Executable)
	if err != nil {
		return blocked("binding_executable_unavailable", fmt.Sprintf("stat verified executable: %v", err))
	}
	if !info.Mode().IsRegular() {
		return blocked("binding_executable_not_regular", "verified executable is not a regular file")
	}
	f, err := os.Open(b.Executable)
	if err != nil {
		return blocked("binding_executable_unavailable", fmt.Sprintf("open verified executable: %v", err))
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return blocked("binding_executable_unavailable", fmt.Sprintf("hash verified executable: %v", err))
	}
	actual := "sha256:" + hex.EncodeToString(h.Sum(nil))
	if actual != b.MaterialDigest {
		return blocked("binding_digest_mismatch", fmt.Sprintf("verified executable digest mismatch: expected %s, got %s", b.MaterialDigest, actual))
	}
	return nil
}

func validateDigest(value string) error {
	if err := validateSHA256(value); err != nil {
		return fmt.Errorf("materialDigest %w", err)
	}
	return nil
}

func validateConfigurationDigest(value string) error {
	if err := validateSHA256(value); err != nil {
		return fmt.Errorf("configurationDigest %w", err)
	}
	return nil
}

func validateSHA256(value string) error {
	hexDigest := strings.TrimPrefix(value, "sha256:")
	if !strings.HasPrefix(value, "sha256:") || len(hexDigest) != 64 || hexDigest != strings.ToLower(hexDigest) {
		return errors.New("must be sha256:<64 lowercase hex characters>")
	}
	if _, err := hex.DecodeString(hexDigest); err != nil {
		return errors.New("contains invalid hexadecimal data")
	}
	return nil
}
