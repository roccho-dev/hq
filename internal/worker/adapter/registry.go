package adapter

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

const RegistryVersion = "adapter.registry.v1"

type Registration struct {
	Target string
	Adapter Adapter
	Provider *ProviderDescriptor
}

type Registry struct {
	registrations map[string]Registration
	targets []string
}

func NewRegistry(registrations ...Registration) (*Registry, error) {
	entries := make(map[string]Registration, len(registrations))
	for _, registration := range registrations {
		if !IsCanonicalTarget(registration.Target) { return nil, fmt.Errorf("target %q is not part of instruction.v1", registration.Target) }
		if isNilAdapter(registration.Adapter) { return nil, fmt.Errorf("adapter for target %q is nil", registration.Target) }
		if _, exists := entries[registration.Target]; exists { return nil, fmt.Errorf("adapter target %q is registered twice", registration.Target) }
		if registration.Provider != nil {
			if err := registration.Provider.Validate(); err != nil { return nil, fmt.Errorf("provider for target %q: %w", registration.Target, err) }
			copy := *registration.Provider
			registration.Provider = &copy
		}
		entries[registration.Target] = registration
	}
	targets := make([]string, 0, len(entries))
	for target := range entries { targets = append(targets, target) }
	sort.Strings(targets)
	return &Registry{registrations: entries, targets: targets}, nil
}

func (r *Registry) Resolve(target string) (Adapter, error) {
	registration, err := r.ResolveRegistration(target)
	if err != nil { return nil, err }
	return registration.Adapter, nil
}

func (r *Registry) ResolveRegistration(target string) (Registration, error) {
	if r == nil { return Registration{}, errors.New("adapter registry is nil") }
	if !IsCanonicalTarget(target) { return Registration{}, &UnknownTargetError{Target: target} }
	resolved, ok := r.registrations[target]
	if !ok { return Registration{}, &AdapterUnavailableError{Target: target} }
	if resolved.Provider != nil { copy := *resolved.Provider; resolved.Provider = &copy }
	return resolved, nil
}

func (r *Registry) Snapshot() RegistrySnapshot {
	if r == nil { return RegistrySnapshot{Version: RegistryVersion, Targets: []string{}} }
	return RegistrySnapshot{Version: RegistryVersion, Targets: append([]string(nil), r.targets...)}
}

type RegistrySnapshot struct { Version string `json:"version"`; Targets []string `json:"targets"` }
type UnknownTargetError struct{ Target string }
func (e *UnknownTargetError) Error() string { return fmt.Sprintf("target %q is not part of instruction.v1", e.Target) }
type AdapterUnavailableError struct{ Target string }
func (e *AdapterUnavailableError) Error() string { return fmt.Sprintf("no adapter registered for canonical target %q", e.Target) }

type FailureClass string
const ( FailureFailed FailureClass = "failed"; FailureBlocked FailureClass = "blocked" )

type FailureError struct { Class FailureClass; Code string; Message string; Retryable bool }
func (e *FailureError) Error() string { return e.Message }
func (e *FailureError) Validate() error {
	if e == nil { return errors.New("failure is nil") }
	if e.Class != FailureFailed && e.Class != FailureBlocked { return fmt.Errorf("invalid failure class %q", e.Class) }
	if strings.TrimSpace(e.Code) == "" || strings.TrimSpace(e.Message) == "" { return errors.New("failure code and message are required") }
	return nil
}
func NewBlockedError(code, message string) error {
	if strings.TrimSpace(code) == "" { code = "policy_blocked" }
	return &FailureError{Class: FailureBlocked, Code: code, Message: message}
}

func isNilAdapter(candidate Adapter) bool {
	if candidate == nil { return true }
	value := reflect.ValueOf(candidate)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default: return false
	}
}
