# Verified-resource invocation

Authority: roccho-dev/adrs#211 latest-writer comment `4978001609`, roccho-dev/adrs#222, and hq#142.

## Product boundary

A selected `hq.local-tool.v1` resource may expose either or both:

- finite named actions for repeated typed operations;
- one resource-level invocation policy for ordered `argv[]` work.

Both lower to the existing `instruction.v1` target `local-tool` and use the same accepted ledger, exact instruction-digest approval, managed worker, verified binding registry, direct process executor, redaction, cancellation, recovery, and `result.v1`.

```text
finite action -------------------+
                                  |
verified resource + argv[] -------+-> instruction.v1
                                      -> approval policy
                                      -> local-tool preparer
                                      -> exact verified binding
                                      -> direct process API
                                      -> result.v1
```

No second queue, worker lifecycle, executor authority, or result vocabulary exists.

## Accepted invocation payload

```json
{
  "tool_id": "aws",
  "tool_version": "2.35.11",
  "policy_version": "aws-restricted.v1",
  "argv": ["sts", "get-caller-identity", "--output", "json"]
}
```

The payload contains no executable path, binding path, deployment ID, environment, credential, profile, account, configuration, working directory, stdin, or shell command string.

`policy_version` is part of the exact instruction digest. A changed policy therefore requires a newly accepted instruction and approval.

## Held and runnable work

A valid invocation with no current exact approval produces:

- one `worker.policy.v1` row with `status=approval_required`;
- one `result.v1` `accepted` row;
- zero provider preparation;
- zero `started` row;
- zero process start;
- no terminal blocked result.

That accepted run remains queued. Re-polling without any state change appends no duplicate held evidence. When an approval matching the exact instruction digest appears, the same run ID becomes runnable and proceeds through the normal worker lifecycle.

A stale approval, invalid payload, resource-policy rejection, missing resource, binding mismatch, or executable tamper is not held. It remains non-green and starts zero process.

## Resource policy

The selected world owns a bounded invocation policy:

- stable `policy_version`;
- maximum argument count and total UTF-8 bytes;
- timeout, stdout, and stderr bounds;
- denied authority-changing options, including both `--flag value` and `--flag=value` forms;
- denied argument prefixes such as response files or `file://` indirection.

The worker also rejects empty/NUL arguments and known secret-shaped material. This is a defensive check, not permission to place secrets in queue input.

The first AWS resource must use a binding-owned restricted identity and empty inherited environment. Queue input cannot change identity or endpoint authority.

## Fixed exclusions

- shell parsing, quoting language, expansion, redirection, pipe, glob, or substitution;
- executable path or PATH/filesystem discovery;
- queue-owned cwd, environment, credential, account, profile, or configuration;
- stdin, PTY, TUI, or interactive authentication;
- generic SSH or OCI execution;
- workflow graphs or automatic agent submit;
- automatic promotion into finite actions.

## Promotion

Resource invocation removes per-subcommand integration waiting. It is not the final form for every repeated operation.

A repeatedly valuable operation may be reviewed and promoted to a finite named action when typed inputs, stable output meaning, or lower-supervision execution reduce total risk and concepts. Promotion is never automatic.

## Closure evidence

The implementation PR must prove deterministically:

1. one resource definition prepares at least three distinct AWS-shaped argv vectors with zero actions;
2. literal shell punctuation remains one argument;
3. denied profile, endpoint, response-file, file-indirection, secret-shaped, limit, policy-drift, stale-approval, unknown-resource, stale-binding, and executable-tamper cases start zero process;
4. missing approval remains durably held and later exact approval resumes the same run exactly once;
5. existing finite-action behavior remains green.

Closing adrs#222 additionally requires a physical restricted-AWS proof and final exact readback. This repository PR does not manufacture cloud evidence.