# host.open one-way launch handoff

Issue owners: #95 and #97

`host.open.v1` is a one-way GUI launch/handoff contract. It is not a
wait-for-provider-process-exit contract.

```text
validated canonical host.open instruction
  -> exact verified provider path
  -> re-hash immediately before launch
  -> direct separated argv
  -> OS process Start
  -> release local process handle
  -> canonical completed handoff
```

## Fixed meaning

A canonical `completed` result means the verified provider process was created
successfully and the OS accepted the launch handoff. It does not assert that hq
waited for or interpreted the provider's later exit status.

This distinction is required for Explorer-shaped GUI providers: the requested
path can be handed to an existing GUI session while the newly created process
later exits non-zero. Waiting for that exit produces false-negative
`provider_failed` evidence after the external handoff already occurred.

## Before-effect failures

The adapter returns non-green evidence before process creation when any of these
fail:

- request/target/operation validation;
- capability identity;
- absolute target path and target availability;
- exact provider integrity verification;
- idempotency contract requirements;
- cancellation already visible at the effect boundary;
- process `Start` itself.

`Start` failure maps to `failed/provider_failed` and produces no successful
completion.

## After-effect boundary

After successful `Start`, a local process-handle release warning is diagnostic
only. It cannot be converted into a result claiming that provider launch never
occurred.

The adapter uses:

- the exact absolute executable from the verified capability binding;
- one cleaned target path as one argv element;
- no shell, PowerShell, cmd, PATH lookup, environment-based provider discovery,
  or command-string parsing.

## Proof boundary

Deterministic tests prove:

- successful handoff returns before a helper provider later exits non-zero;
- a target path containing spaces remains one argv element;
- poisoned PATH is irrelevant;
- invalid cwd makes `Start` fail before the helper marker exists;
- pre-effect cancellation launches no provider;
- managed-worker integration observes both canonical completion and the helper
  marker.

A new pc7337 installed proof is intentionally deferred until the current W1
host owner releases the mutable host. Existing native evidence is historical
compatibility evidence, not the exact-head merge gate for this rebased change.
