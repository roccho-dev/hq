package current

import (
	"fmt"

	"hq/internal/core"
)

func validateLocalToolBindings(tool core.LocalToolDefinition) (map[string]core.LocalToolBinding, error) {
	byName := make(map[string]core.LocalToolBinding, len(tool.Bindings))
	byRef := make(map[string]struct{}, len(tool.Bindings))
	for index, binding := range tool.Bindings {
		for field, value := range map[string]string{
			"name": binding.Name, "binding_ref": binding.BindingRef, "binding_contract_version": binding.BindingContractVersion,
		} {
			if !validLocalToolName(value) {
				return nil, fmt.Errorf("binding %d %s is invalid", index+1, field)
			}
		}
		if _, duplicate := byName[binding.Name]; duplicate {
			return nil, fmt.Errorf("duplicate dependency binding name %q", binding.Name)
		}
		if binding.BindingRef == tool.BindingRef {
			return nil, fmt.Errorf("binding %q must not reuse the primary binding_ref", binding.Name)
		}
		if _, duplicate := byRef[binding.BindingRef]; duplicate {
			return nil, fmt.Errorf("duplicate dependency binding_ref %q", binding.BindingRef)
		}
		byName[binding.Name] = binding
		byRef[binding.BindingRef] = struct{}{}
	}
	return byName, nil
}
