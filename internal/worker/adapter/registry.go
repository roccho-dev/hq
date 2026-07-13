package adapter

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

const RegistryVersion = "adapter.registry.v1"

type Registration struct {
	Target   string
	Adapter  Adapter
	Preparer Preparer
	Provider *ProviderDescriptor
}
type Registry struct {
	registrations map[string]Registration
	targets       []string
}

func NewRegistry(rs ...Registration) (*Registry, error) {
	entries := map[string]Registration{}
	for _, r := range rs {
		if !IsCanonicalTarget(r.Target) {
			return nil, fmt.Errorf("target %q is not part of instruction.v1", r.Target)
		}
		static := !isNilAdapter(r.Adapter)
		dynamic := !isNilPreparer(r.Preparer)
		if static == dynamic {
			return nil, fmt.Errorf("target %q must register exactly one of adapter or preparer", r.Target)
		}
		if dynamic && r.Provider != nil {
			return nil, fmt.Errorf("dynamic target %q must provide provider evidence during preparation", r.Target)
		}
		if _, ok := entries[r.Target]; ok {
			return nil, fmt.Errorf("adapter target %q is registered twice", r.Target)
		}
		if r.Provider != nil {
			if err := r.Provider.Validate(); err != nil {
				return nil, fmt.Errorf("provider for target %q: %w", r.Target, err)
			}
			copy := *r.Provider
			r.Provider = &copy
		}
		entries[r.Target] = r
	}
	targets := make([]string, 0, len(entries))
	for t := range entries {
		targets = append(targets, t)
	}
	sort.Strings(targets)
	return &Registry{entries, targets}, nil
}
func (r *Registry) Resolve(t string) (Adapter, error) {
	x, err := r.ResolveRegistration(t)
	if err != nil {
		return nil, err
	}
	if !isNilPreparer(x.Preparer) {
		return nil, fmt.Errorf("target %q requires request preparation", t)
	}
	return x.Adapter, nil
}
func (r *Registry) ResolveRegistration(t string) (Registration, error) {
	if r == nil {
		return Registration{}, errors.New("adapter registry is nil")
	}
	if !IsCanonicalTarget(t) {
		return Registration{}, &UnknownTargetError{t}
	}
	x, ok := r.registrations[t]
	if !ok {
		return Registration{}, &AdapterUnavailableError{t}
	}
	if x.Provider != nil {
		copy := *x.Provider
		x.Provider = &copy
	}
	return x, nil
}

// Prepare normalizes static and dynamic registrations into the exact adapter
// and provider that Runner may record and dispatch for this request.
func (r Registration) Prepare(ctx context.Context, request Request) (Prepared, error) {
	var prepared Prepared
	var err error
	if !isNilPreparer(r.Preparer) {
		prepared, err = r.Preparer.Prepare(ctx, request)
		if err != nil {
			return Prepared{}, err
		}
	} else {
		prepared.Adapter = r.Adapter
		if r.Provider != nil {
			provider := *r.Provider
			prepared.Provider = &provider
		}
	}
	if isNilAdapter(prepared.Adapter) {
		return Prepared{}, NewBlockedError("provider_prepare_invalid", "preparation returned a nil adapter")
	}
	if !isNilPreparer(r.Preparer) && prepared.Provider == nil {
		return Prepared{}, NewBlockedError("provider_prepare_invalid", "dynamic preparation requires provider evidence")
	}
	if prepared.Provider != nil {
		if err := prepared.Provider.Validate(); err != nil {
			return Prepared{}, NewBlockedError("provider_prepare_invalid", err.Error())
		}
		provider := *prepared.Provider
		prepared.Provider = &provider
	}
	return prepared, nil
}
func (r *Registry) Snapshot() RegistrySnapshot {
	if r == nil {
		return RegistrySnapshot{RegistryVersion, []string{}}
	}
	return RegistrySnapshot{RegistryVersion, append([]string(nil), r.targets...)}
}

type RegistrySnapshot struct {
	Version string   `json:"version"`
	Targets []string `json:"targets"`
}
type UnknownTargetError struct{ Target string }

func (e *UnknownTargetError) Error() string {
	return fmt.Sprintf("target %q is not part of instruction.v1", e.Target)
}

type AdapterUnavailableError struct{ Target string }

func (e *AdapterUnavailableError) Error() string {
	return fmt.Sprintf("no adapter registered for canonical target %q", e.Target)
}

type FailureClass string

const (
	FailureFailed  FailureClass = "failed"
	FailureBlocked FailureClass = "blocked"
)

type FailureError struct {
	Class     FailureClass
	Code      string
	Message   string
	Retryable bool
}

func (e *FailureError) Error() string { return e.Message }
func (e *FailureError) Validate() error {
	if e == nil {
		return errors.New("failure is nil")
	}
	if e.Class != FailureFailed && e.Class != FailureBlocked {
		return fmt.Errorf("invalid failure class %q", e.Class)
	}
	if strings.TrimSpace(e.Code) == "" || strings.TrimSpace(e.Message) == "" {
		return errors.New("failure code and message are required")
	}
	return nil
}
func NewBlockedError(c, m string) error {
	if strings.TrimSpace(c) == "" {
		c = "policy_blocked"
	}
	return &FailureError{Class: FailureBlocked, Code: c, Message: m}
}
func isNilAdapter(a Adapter) bool {
	if a == nil {
		return true
	}
	v := reflect.ValueOf(a)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

func isNilPreparer(p Preparer) bool {
	if p == nil {
		return true
	}
	v := reflect.ValueOf(p)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}
