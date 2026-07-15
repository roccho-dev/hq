package localtoolpolicy

import (
	"errors"
	"strings"
	"testing"

	"hq/internal/core"
)

func testPolicy() core.LocalToolInvocation {
	return core.LocalToolInvocation{
		PolicyVersion: "aws-restricted.v1",
		DeniedOptions: []string{
			"--profile", "--endpoint-url", "--ca-bundle", "--no-sign-request",
			"--no-verify-ssl", "--debug", "--cli-input-json", "--cli-input-yaml",
		},
		DeniedArgumentPrefixes: []string{"file://", "fileb://", "@", "configure", "login", "sso"},
		MaxArgv:               32,
		MaxArgBytes:           4096,
		Limits:                core.LocalToolLimits{TimeoutMS: 60000, StdoutBytes: 1 << 20, StderrBytes: 1 << 20},
	}
}

func TestDefinitionAndDistinctAWSInvocations(t *testing.T) {
	policy := testPolicy()
	if err := ValidateDefinition(policy); err != nil {
		t.Fatal(err)
	}
	for _, argv := range [][]string{
		{"sts", "get-caller-identity", "--output", "json", "--no-cli-pager"},
		{"apigateway", "get-rest-api", "--rest-api-id", "api-123", "--no-cli-pager"},
		{"iam", "list-roles", "--no-cli-pager"},
	} {
		if err := ValidateInvocation(policy, policy.PolicyVersion, argv); err != nil {
			t.Fatalf("argv=%q err=%v", argv, err)
		}
	}
}

func TestInvocationAllowsLiteralShellPunctuation(t *testing.T) {
	policy := testPolicy()
	argument := `;&|$()^%PATH%`
	if err := ValidateInvocation(policy, policy.PolicyVersion, []string{"sts", "get-caller-identity", argument}); err != nil {
		t.Fatalf("literal argument was treated as syntax: %v", err)
	}
}

func TestInvocationRejectsAuthorityEscapeAndSecretArguments(t *testing.T) {
	policy := testPolicy()
	for _, test := range []struct {
		name string
		argv []string
		code string
	}{
		{"profile separate", []string{"sts", "get-caller-identity", "--profile", "admin"}, "resource_invocation_option_denied"},
		{"profile equals", []string{"sts", "get-caller-identity", "--profile=admin"}, "resource_invocation_option_denied"},
		{"endpoint", []string{"sts", "get-caller-identity", "--endpoint-url=https://example.invalid"}, "resource_invocation_option_denied"},
		{"tls bypass", []string{"sts", "get-caller-identity", "--no-verify-ssl"}, "resource_invocation_option_denied"},
		{"debug output", []string{"sts", "get-caller-identity", "--debug"}, "resource_invocation_option_denied"},
		{"response file", []string{"sts", "get-caller-identity", "@request.json"}, "resource_invocation_argument_denied"},
		{"text file indirection", []string{"sts", "get-caller-identity", "file://secret.json"}, "resource_invocation_argument_denied"},
		{"binary file indirection", []string{"sts", "get-caller-identity", "fileb://secret.bin"}, "resource_invocation_argument_denied"},
		{"configure", []string{"configure", "set", "profile.admin.region", "us-east-1"}, "resource_invocation_argument_denied"},
		{"login", []string{"login", "--remote"}, "resource_invocation_argument_denied"},
		{"sso auth", []string{"sso", "login"}, "resource_invocation_argument_denied"},
		{"access key", []string{"sts", "get-caller-identity", "AKIA" + strings.Repeat("A", 16)}, "resource_invocation_secret_denied"},
		{"session token", []string{"sts", "get-caller-identity", "aws_session_token=secret"}, "resource_invocation_secret_denied"},
		{"policy drift", []string{"sts", "get-caller-identity"}, "resource_invocation_policy_mismatch"},
	} {
		t.Run(test.name, func(t *testing.T) {
			version := policy.PolicyVersion
			if test.name == "policy drift" {
				version = "stale.v1"
			}
			err := ValidateInvocation(policy, version, test.argv)
			var failure *Failure
			if !errors.As(err, &failure) || failure.Code != test.code {
				t.Fatalf("err=%v failure=%+v want=%s", err, failure, test.code)
			}
		})
	}
}

func TestInvocationLimitsFailClosed(t *testing.T) {
	policy := testPolicy()
	policy.MaxArgv = 2
	if err := ValidateInvocation(policy, policy.PolicyVersion, []string{"a", "b", "c"}); err == nil {
		t.Fatal("argument-count limit was ignored")
	}
	policy = testPolicy()
	policy.MaxArgBytes = 4
	if err := ValidateInvocation(policy, policy.PolicyVersion, []string{"abc", "de"}); err == nil {
		t.Fatal("argument-byte limit was ignored")
	}
}
