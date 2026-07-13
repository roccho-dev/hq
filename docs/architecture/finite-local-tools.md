# Finite local tools

`hq.local-tool.v1` is the first bounded implementation child of
`roccho-dev/adrs#211`. It adds one generic target to the existing canonical
instruction, managed-worker, adapter, and `result.v1` path. It does not add a
queue, runner lifecycle, or durable evidence family.

The ownership boundary is:

```text
selected world JSONL
  hq.local-tool.v1 (meaning, action, bounds)
    -> stable binding_ref + expected binding_contract_version

selected profile
  absolute world_path
  absolute executable_bindings_path
    -> envctl.verified-executable-bindings.v1
    -> immutable absolute executable + digest + deployment identity

managed worker
  Prepare(request) verifies exact action/input/binding/digest
    -> durable started result.v1 with exact provider evidence
    -> re-hash immediately before direct process launch
    -> bounded stdout/stderr/stdin/timeout
    -> canonical terminal result.v1
```

World data never contains an executable path, command string, shell syntax,
PATH lookup, working-directory discovery, environment inheritance, branches,
loops, or a workflow graph. The queued payload contains only exact tool ID,
tool version, action ID, and typed input. The binding reference remains world
data and is not copied into the queue.

## First-child finite semantics

- `argv` entries are exactly one `literal` or one required typed `field`.
- input types are `string`, `integer`, `boolean`, and finite `enum`.
- stdin is `none` or one required string field with an explicit byte limit.
- output is `text`, one JSON value, or JSONL.
- a native session selector is a fixed list of object-field segments. For JSON
  it reads the sole value; for JSONL it reads the last non-empty record. It is
  not JSONPath, a regular expression, or an expression language.
- lifecycle is `one-shot` only. Continuation, cancellation, and run-view
  classes remain reserved until their generic lifecycle semantics are proved.
- approval is `explicit` only. The existing digest-bound worker approval is
  intentionally stronger than local-tool risk metadata.
- the direct executor receives an explicitly empty environment and a verified
  absolute path. Metacharacters are literal argv bytes; no shell is involved.

`binding_contract_version` is required. An envs profile may supersede the same
`binding_ref`; hq rejects a registry entry whose contract version differs from
the world expectation instead of silently accepting the replacement.

## Migration boundary

Dedicated Herdr, Codex, and Claude adapters remain registered and unchanged.
This child proves a generic one-shot path only. Complete Herdr action parity,
generic run-view projection, Vim view operations, and Codex/Claude migration
belong to later ADR #211 children.

The unrelated dummy and Herdr `--version` definitions in
`examples/hq.local-tools.jsonl` demonstrate that adding a binary changes world
and envs binding data, not generic hq core or Vim code.
