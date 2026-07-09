# Worker safety boundary

`hq` compiles and accepts JSONL. A worker may execute only after it composes this package's project-local layout, durable-log redaction, and fail-closed approval decision.

## Purpose lineage

| Generation | Purpose | Direct contribution |
|---|---|---|
| Scope | Close `hq#75`, `hq#76`, and `hq#77`. | One layout, one redactor, one approval decision. |
| Product | Keep the compiler small while making downstream execution safe to operate. | Safety is a replaceable worker contract, not compiler meaning. |
| System | Make queue, events, sessions, outputs, proofs, secret handling, and dispatch decisions predictable. | Pure functions expose paths, redacted records, and `may_dispatch`. |
| Organization | Let separate worker and adapter owners share one boundary. | No target-specific branch or duplicated safety rule is needed. |
| Business | Reduce setup, incident, review, and handoff cost for parallel local agents. | Durable evidence becomes easier to retain and inspect. |
| Company | Build a small automation asset that can be operated without its original author. | The boundary is documented and mechanically tested. |
| Meta | Prevent convenience paths from weakening evidence or human control. | Default deny, digest-bound approval, and non-mutating redaction are explicit. |
| Meta^10 | Increase sale value by lowering hidden operational and diligence risk. | A buyer can reconstruct where data lives, what was masked, and why dispatch was allowed. |

## Canonical project-local layout

`workersafety.NewLayout(projectRoot)` resolves this convention without filesystem effects:

```text
<project>/
  .hq/
    queue/
      instructions.jsonl
    events/
      events.jsonl
    sessions/
      sessions.jsonl
    outputs/
      <run_id>/
        final.json
    proofs/
      <run_id>.jsonl
```

Every path is below one project-local `.hq/` root. Run identifiers must be one path segment; traversal and alternate paths fail validation. This repository ignores its own root `.hq/` runtime data, while checked-in fixtures remain outside that directory.

## Durable-data order

```text
instruction row
  -> contract validation          # worker issue #43
  -> workersafety.EvaluatePolicy
  -> workersafety.RedactRecord(policy event)
  -> append worker.policy.v1
  -> dispatch only when may_dispatch=true
  -> workersafety.RedactRecord(result event/final metadata)
  -> append event/final artifact  # worker issue #45
```

The package does not launch a process, write a file, append JSONL, fetch secrets, or grant authority. It returns pure decisions for the worker's effect adapters.

## Redaction contract

`RedactRecord` deep-copies JSON-compatible data and masks obvious secrets before durable append.

| Surface | Covered behavior |
|---|---|
| `env` and payload fields | Secret-bearing keys such as token, password, authorization, API key, access key, private key, and client secret are replaced. |
| stdout / stderr / final metadata | Bearer tokens, common GitHub/OpenAI/AWS token forms, secret-like environment assignments, and private-key blocks are masked. |
| Debug identity | `kind`, `status`, run/instruction ids, target, operation, native session id, final path, and creation time remain available. |
| JSON shape | Maps and arrays remain valid JSON-compatible structures; the input object is not mutated. |

This is a bounded safeguard, not full data-loss prevention or cryptographic secret management. Unknown secret forms can still require an adapter-specific pre-filter. The invariant is that all durable worker events and final metadata pass through this function or an explicitly stronger replacement.

## Approval contract

The zero-value policy denies dispatch.

| Risk / policy | Approval | Status | `may_dispatch` |
|---|---|---|---:|
| safe + `allow_auto` | none | `allowed` | true |
| safe + no mode | none | `blocked` | false |
| dangerous + `allow_auto` | any | `blocked` | false |
| any + `require_approval` | missing | `approval_required` | false |
| any + `require_approval` | stale digest | `blocked` | false |
| any + `require_approval` | actor + exact instruction digest | `allowed` | true |
| any + `block` | any | `blocked` | false |
| conflicting modes / unknown risk / invalid request | any | `blocked` | false |

A decision is emitted as `worker.policy.v1`, including instruction/run identity, instruction digest, risk, status, reason, approval actor when present, and `may_dispatch`. It is evidence only. Approval is bound to the exact instruction digest so changing the request invalidates earlier consent.

## Ownership and non-scope

| Layer | Owner here | Not owned here |
|---|---|---|
| Pure core | Path convention, path validation, redaction, policy evaluation. | Filesystem, clocks, process execution, adapter commands. |
| Worker port | `Layout`, redacted JSON-compatible record, `PolicyDecision`. | Queue reader/result schema implementation owned by other issues. |
| Effect adapter | None. | Directory creation, JSONL append, shell/Herdr/Codex/Claude execution, UI approval. |
| Authority | None; policy events are evidence. | Accepted ledger, governance admission, remote SSOT promotion. |

## Mechanical proof and breaking cases

The package tests prove:

- all queue/event/session/output/proof paths use one root and traversal fails;
- env, payload, stdout, stderr, and final metadata examples do not retain known secrets;
- valid JSON structure and debugging identity survive redaction;
- dangerous auto-run, missing approval, stale approval, conflicting policy, unknown risk, and malformed requests all produce `may_dispatch=false`;
- exact explicit approval is the only dangerous path that can dispatch.

The design is intentionally one package rather than three feature packages: layout, redaction, and approval are independent functions but form one worker-side safety boundary. This minimizes repeated wiring while keeping each rule separately testable.
