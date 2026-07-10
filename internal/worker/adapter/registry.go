package adapter

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
)

const RegistryVersion = "adapter.registry.v1"

type Registration struct {
	Target  string
	Adapter Adapter
}

type Registry struct {
	adapters map[string]Adapter
	targets  []string
}

var runtimeDefaults struct {
	sync.RWMutex
	factory func() []Registration
}

// InstallRuntimeDefaults lets one executable compose its concrete adapters
// without moving provider meaning into worker core. Packages that do not install
// a factory keep the original empty-registry behavior.
func InstallRuntimeDefaults(factory func() []Registration) error {
	if factory == nil {
		return errors.New("runtime adapter factory is nil")
	}
	runtimeDefaults.Lock()
	defer runtimeDefaults.Unlock()
	if runtimeDefaults.factory != nil {
		return errors.New("runtime adapter defaults are already installed")
	}
	runtimeDefaults.factory = factory
	return nil
}

func NewRegistry(registrations ...Registration) (*Registry, error) {
	if len(registrations) == 0 {
		runtimeDefaults.RLock()
		factory := runtimeDefaults.factory
		runtimeDefaults.RUnlock()
		if factory != nil {
			registrations = factory()
		}
	}
	adapters := make(map[string]Adapter, len(registrations))
	for _, registration := range registrations {
		if !IsCanonicalTarget(registration.Target) {
			return nil, fmt.Errorf("target %q is not part of instruction.v1", registration.Target)
		}
		if isNilAdapter(registration.Adapter) {
			return nil, fmt.Errorf("adapter for target %q is nil", registration.Target)
		}
		if _, exists := adapters[registration.Target]; exists {
			return nil, fmt.Errorf("adapter target %q is registered twice", registration.Target)
		}
		adapters[registration.Target] = registration.Adapter
	}
	targets := make([]string, 0, len(adapters))
	for target := range adapters {
		targets = append(targets, target)
	}
	sort.Strings(targets)
	return &Registry{adapters: adapters, targets: targets}, nil
}

func (r *Registry) Resolve(target string) (Adapter, error) {
	if r == nil {
		return nil, errors.New("adapter registry is nil")
	}
	if !IsCanonicalTarget(target) {
		return nil, &UnknownTargetError{Target: target}
	}
	resolved, ok := r.adapters[target]
	if !ok {
		return nil, &AdapterUnavailableError{Target: target}
	}
	return resolved, nil
}

func (r *Registry) Snapshot() RegistrySnapshot {
	if r == nil {
		return RegistrySnapshot{Version: RegistryVersion, Targets: []string{}}
	}
	return RegistrySnapshot{Version: RegistryVersion, Targets: append([]string(nil), r.targets...)}
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

// FailureError lets an adapter return a stable transient failure without
// owning the durable result.v1 envelope or lifecycle identity.
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

func NewBlockedError(code, message string) error {
	if strings.TrimSpace(code) == "" {
		code = "policy_blocked"
	}
	return &FailureError{Class: FailureBlocked, Code: code, Message: message}
}

func isNilAdapter(candidate Adapter) bool {
	if candidate == nil {
		return true
	}
	value := reflect.ValueOf(candidate)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
