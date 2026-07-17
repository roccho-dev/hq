package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// UnmarshalJSON keeps the selected-world wire strict while adapting the new
// binding_executable atom to the original finite-argv validator. MarshalJSON on
// LocalToolArg removes that internal compatibility alias again, so canonical
// world bytes contain exactly one tagged-union member.
func (tool *LocalToolDefinition) UnmarshalJSON(data []byte) error {
	type wire LocalToolDefinition
	var decoded wire
	if err := decodeLocalToolWire(data, &decoded); err != nil {
		return err
	}
	candidate := LocalToolDefinition(decoded)
	if err := validateLocalToolBindingWire(candidate); err != nil {
		return err
	}
	*tool = candidate
	return nil
}

func (argument *LocalToolArg) UnmarshalJSON(data []byte) error {
	var decoded struct {
		Literal           *string `json:"literal,omitempty"`
		Field             *string `json:"field,omitempty"`
		BindingExecutable *string `json:"binding_executable,omitempty"`
	}
	if err := decodeLocalToolWire(data, &decoded); err != nil {
		return err
	}
	members := 0
	for _, value := range []*string{decoded.Literal, decoded.Field, decoded.BindingExecutable} {
		if value != nil {
			members++
			if *value == "" {
				return errors.New("local-tool argv member must not be empty")
			}
		}
	}
	if members != 1 {
		return errors.New("local-tool argv must declare exactly one of literal, field, or binding_executable")
	}
	*argument = LocalToolArg{Literal: decoded.Literal, Field: decoded.Field, BindingExecutable: decoded.BindingExecutable}
	if decoded.BindingExecutable != nil {
		// The merged first-child adapter validator knows literal and field. Keep a
		// private literal alias only in memory; MarshalJSON never emits it.
		argument.Literal = decoded.BindingExecutable
	}
	return nil
}

func (argument LocalToolArg) MarshalJSON() ([]byte, error) {
	if argument.BindingExecutable != nil {
		return json.Marshal(struct {
			BindingExecutable *string `json:"binding_executable"`
		}{BindingExecutable: argument.BindingExecutable})
	}
	return json.Marshal(struct {
		Literal *string `json:"literal,omitempty"`
		Field   *string `json:"field,omitempty"`
	}{Literal: argument.Literal, Field: argument.Field})
}

func validateLocalToolBindingWire(tool LocalToolDefinition) error {
	byName := make(map[string]LocalToolBinding, len(tool.Bindings))
	byRef := make(map[string]struct{}, len(tool.Bindings))
	previous := ""
	for index, binding := range tool.Bindings {
		for field, value := range map[string]string{
			"name": binding.Name, "binding_ref": binding.BindingRef, "binding_contract_version": binding.BindingContractVersion,
		} {
			if !validLocalToolWireName(value) {
				return fmt.Errorf("local-tool binding %d %s is invalid", index+1, field)
			}
		}
		if previous != "" && binding.Name <= previous {
			return errors.New("local-tool binding names must be unique and sorted")
		}
		previous = binding.Name
		if _, duplicate := byRef[binding.BindingRef]; duplicate {
			return fmt.Errorf("duplicate local-tool dependency binding_ref %q", binding.BindingRef)
		}
		byName[binding.Name] = binding
		byRef[binding.BindingRef] = struct{}{}
	}
	for _, action := range tool.Actions {
		for _, argument := range action.Argv {
			if argument.BindingExecutable == nil {
				continue
			}
			if _, ok := byName[*argument.BindingExecutable]; !ok {
				return fmt.Errorf("action %q references unknown binding_executable %q", action.ActionID, *argument.BindingExecutable)
			}
		}
	}
	return nil
}

func validLocalToolWireName(value string) bool {
	if value == "" || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '.' || character == '_' || character == '-' {
			continue
		}
		return false
	}
	return true
}

func decodeLocalToolWire(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("local-tool record contains multiple JSON values")
		}
		return err
	}
	return nil
}
