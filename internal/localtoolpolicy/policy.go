package localtoolpolicy

import (
	"errors"
	"fmt"
	"strings"

	"hq/internal/core"
)

const (
	MaxArgv     = 128
	MaxArgBytes = 64 << 10
)

type Failure struct {
	Code    string
	Message string
}

func (e *Failure) Error() string { return e.Message }

func ValidateDefinition(policy core.LocalToolInvocation) error {
	if strings.TrimSpace(policy.PolicyVersion) == "" || policy.PolicyVersion != strings.TrimSpace(policy.PolicyVersion) || strings.ContainsAny(policy.PolicyVersion, " \t\r\n") {
		return errors.New("policy_version must be one canonical non-empty token")
	}
	if policy.MaxArgv <= 0 || policy.MaxArgv > MaxArgv {
		return fmt.Errorf("max_argv must be between 1 and %d", MaxArgv)
	}
	if policy.MaxArgBytes <= 0 || policy.MaxArgBytes > MaxArgBytes {
		return fmt.Errorf("max_arg_bytes must be between 1 and %d", MaxArgBytes)
	}
	if err := validateUnique(policy.DeniedOptions, func(value string) bool {
		return len(value) > 2 && strings.HasPrefix(value, "--") && !strings.Contains(value, "=") && !strings.ContainsAny(value, " \t\r\n")
	}, "denied_options"); err != nil {
		return err
	}
	if err := validateUnique(policy.DeniedArgumentPrefixes, func(value string) bool {
		return value != "" && value == strings.TrimSpace(value) && !strings.ContainsAny(value, "\r\n")
	}, "denied_argument_prefixes"); err != nil {
		return err
	}
	return nil
}

func ValidateInvocation(policy core.LocalToolInvocation, policyVersion string, argv []string) error {
	if policyVersion != policy.PolicyVersion {
		return failure("resource_invocation_policy_mismatch", fmt.Sprintf("invocation policy is %q, expected %q", policyVersion, policy.PolicyVersion))
	}
	if len(argv) == 0 {
		return failure("resource_invocation_argv_invalid", "argv must contain at least one argument")
	}
	if len(argv) > policy.MaxArgv {
		return failure("resource_invocation_argv_limit", fmt.Sprintf("argv contains %d arguments, limit is %d", len(argv), policy.MaxArgv))
	}
	total := 0
	for index, argument := range argv {
		if argument == "" || strings.TrimSpace(argument) == "" || strings.ContainsRune(argument, '\x00') {
			return failure("resource_invocation_argv_invalid", fmt.Sprintf("argv[%d] must be a non-empty string without NUL", index))
		}
		total += len([]byte(argument))
		if total > policy.MaxArgBytes {
			return failure("resource_invocation_argv_limit", fmt.Sprintf("argv exceeds %d bytes", policy.MaxArgBytes))
		}
		for _, denied := range policy.DeniedOptions {
			if matchesDeniedOption(argument, denied) {
				return failure("resource_invocation_option_denied", fmt.Sprintf("argv[%d] uses denied option or abbreviation of %q", index, denied))
			}
		}
		for _, prefix := range policy.DeniedArgumentPrefixes {
			if strings.HasPrefix(argument, prefix) {
				return failure("resource_invocation_argument_denied", fmt.Sprintf("argv[%d] uses denied argument prefix %q", index, prefix))
			}
		}
		if looksSensitive(argument) {
			return failure("resource_invocation_secret_denied", fmt.Sprintf("argv[%d] resembles secret material", index))
		}
	}
	return nil
}

// Python argparse accepts unambiguous long-option prefixes unless a caller
// disables abbreviation. Resource policy therefore rejects both the exact
// dangerous option and every syntactic long-option prefix that could resolve
// to it. An ambiguous prefix is also safely rejected.
func matchesDeniedOption(argument, denied string) bool {
	name := argument
	if index := strings.IndexByte(name, '='); index >= 0 {
		name = name[:index]
	}
	return len(name) > 2 && strings.HasPrefix(name, "--") && strings.HasPrefix(denied, name)
}

func validateUnique(values []string, valid func(string) bool, field string) error {
	seen := map[string]struct{}{}
	for _, value := range values {
		if !valid(value) {
			return fmt.Errorf("%s contains invalid value %q", field, value)
		}
		if _, duplicate := seen[value]; duplicate {
			return fmt.Errorf("%s contains duplicate value %q", field, value)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func looksSensitive(value string) bool {
	lower := strings.ToLower(value)
	for _, fragment := range []string{
		"aws_secret_access_key", "aws_session_token", "authorization:", "bearer ",
		"private_key", "client_secret", "access_token", "refresh_token",
	} {
		if strings.Contains(lower, fragment) {
			return true
		}
	}
	for _, prefix := range []string{"ghp_", "github_pat_", "sk-"} {
		if strings.HasPrefix(lower, prefix) && len(value) >= len(prefix)+16 {
			return true
		}
	}
	if (strings.HasPrefix(value, "AKIA") || strings.HasPrefix(value, "ASIA")) && len(value) == 20 {
		return true
	}
	return false
}

func failure(code, message string) error {
	return &Failure{Code: code, Message: message}
}
