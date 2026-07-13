package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const (
	WorldDefinitionKind       = "hq.world.v1"
	CompileProvenanceKind     = "hq.compile.provenance.v1"
	CommandInputKind          = "hq.command.v1"
	CanonicalJSONInputKind    = "canonical-json"
	DigestAlgorithmPrefix     = "sha256:"
)

// WorldDefinition gives one immutable aggregate world a stable identity. The
// source path and JSONL row order are deployment details and are not identity.
type WorldDefinition struct {
	Kind    string `json:"kind"`
	WorldID string `json:"world_id"`
}

// WorldRef identifies the exact normalized semantic world used by a compiler.
type WorldRef struct {
	WorldID string `json:"world_id"`
	Digest  string `json:"digest"`
}

// CommandRef identifies one exact command contract within a selected world.
type CommandRef struct {
	CommandID      string `json:"command_id"`
	CommandVersion string `json:"command_version"`
	Name           string `json:"name"`
	Digest         string `json:"digest"`
}

// CompileProvenance binds a finalized canonical instruction to the immutable
// world and optional command contract that produced it. It is evidence only;
// Instruction remains the worker execution authority.
type CompileProvenance struct {
	Kind              string      `json:"kind"`
	InputKind         string      `json:"input_kind"`
	World             WorldRef    `json:"world"`
	Command           *CommandRef `json:"command,omitempty"`
	InstructionDigest string      `json:"instruction_digest"`
}

func CanonicalDigest(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return DigestAlgorithmPrefix + hex.EncodeToString(sum[:]), nil
}

// RecomputeDigest records the digest of normalized semantic content. Callers
// must normalize collection order before invoking this method.
func (w *JsonlWorld) RecomputeDigest() error {
	if w == nil {
		return errors.New("world is nil")
	}
	worldID := ""
	if w.Identity != nil {
		worldID = w.Identity.WorldID
	}
	semantic := struct {
		WorldID    string                `json:"world_id,omitempty"`
		Keys       []SchemaKey           `json:"keys,omitempty"`
		Commands   []CommandDefinition   `json:"commands,omitempty"`
		LocalTools []LocalToolDefinition `json:"local_tools,omitempty"`
		Rows       []map[string]any      `json:"rows,omitempty"`
	}{
		WorldID: worldID, Keys: w.Keys, Commands: w.Commands,
		LocalTools: w.LocalTools, Rows: w.Rows,
	}
	digest, err := CanonicalDigest(semantic)
	if err != nil {
		return err
	}
	w.Digest = digest
	return nil
}

func (w *JsonlWorld) SelectedRef() (WorldRef, bool) {
	if w == nil || w.Identity == nil || strings.TrimSpace(w.Identity.WorldID) == "" || !ValidDigest(w.Digest) {
		return WorldRef{}, false
	}
	return WorldRef{WorldID: w.Identity.WorldID, Digest: w.Digest}, true
}

func (w *JsonlWorld) CommandRef(name string) (CommandRef, bool) {
	command, ok := w.Command(name)
	if !ok || strings.TrimSpace(command.CommandID) == "" || strings.TrimSpace(command.CommandVersion) == "" {
		return CommandRef{}, false
	}
	digest, err := CanonicalDigest(command)
	if err != nil {
		return CommandRef{}, false
	}
	return CommandRef{CommandID: command.CommandID, CommandVersion: command.CommandVersion, Name: command.Name, Digest: digest}, true
}

func (w *JsonlWorld) CommandByID(commandID string) (CommandDefinition, bool) {
	if w == nil {
		return CommandDefinition{}, false
	}
	for _, command := range w.Commands {
		if command.CommandID == commandID {
			return command, true
		}
	}
	return CommandDefinition{}, false
}

// ValidateSelected requires the identity contract used by production selected-
// world features. The generic loader still accepts legacy identity-free worlds
// so existing instruction logs remain readable during migration.
func (w *JsonlWorld) ValidateSelected() error {
	if w == nil || w.Identity == nil {
		return errors.New("selected world requires one hq.world.v1 record")
	}
	if w.Identity.Kind != WorldDefinitionKind || !validStableIdentity(w.Identity.WorldID) {
		return errors.New("selected world has invalid world_id")
	}
	if !ValidDigest(w.Digest) {
		return errors.New("selected world has invalid canonical digest")
	}
	seen := map[string]bool{}
	for _, command := range w.Commands {
		if !validStableIdentity(command.CommandID) || !validStableIdentity(command.CommandVersion) {
			return fmt.Errorf("command %q requires command_id and command_version in a selected world", command.Name)
		}
		if seen[command.CommandID] {
			return fmt.Errorf("duplicate command_id %q", command.CommandID)
		}
		seen[command.CommandID] = true
	}
	return nil
}

func (p CompileProvenance) Validate() error {
	if p.Kind != CompileProvenanceKind {
		return fmt.Errorf("provenance kind must be %q", CompileProvenanceKind)
	}
	if p.InputKind != CommandInputKind && p.InputKind != CanonicalJSONInputKind {
		return fmt.Errorf("unsupported provenance input_kind %q", p.InputKind)
	}
	if !validStableIdentity(p.World.WorldID) || !ValidDigest(p.World.Digest) {
		return errors.New("provenance world reference is invalid")
	}
	if !ValidDigest(p.InstructionDigest) {
		return errors.New("provenance instruction_digest is invalid")
	}
	if p.InputKind == CommandInputKind {
		if p.Command == nil {
			return errors.New("command provenance requires command reference")
		}
		if !validStableIdentity(p.Command.CommandID) || !validStableIdentity(p.Command.CommandVersion) || strings.TrimSpace(p.Command.Name) == "" || !ValidDigest(p.Command.Digest) {
			return errors.New("provenance command reference is invalid")
		}
	} else if p.Command != nil {
		return errors.New("canonical-json provenance must not declare a command")
	}
	return nil
}

func ValidDigest(value string) bool {
	if !strings.HasPrefix(value, DigestAlgorithmPrefix) {
		return false
	}
	raw := strings.TrimPrefix(value, DigestAlgorithmPrefix)
	if len(raw) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(raw)
	return err == nil
}

func validStableIdentity(value string) bool {
	if value == "" || strings.TrimSpace(value) != value {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}
