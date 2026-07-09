# Vim-to-hq contract

## Purpose lineage

| Generation | Purpose | Direct contribution |
|---|---|---|
| Scope | Fix the exact Vim-to-hq boundary. | Vim passes buffer, cursor, schema path, queue path, and one explicit action only. |
| Product | Keep editor integration replaceable. | Vim uses the same public `cmd/hq` contract as every other client. |
| System | Keep compilation separate from execution. | `hq` completes, drafts, accepts, and optionally appends one row; workers and adapters remain downstream. |
| Organization | Keep ownership clear. | Editor code owns interaction, `hq` owns compilation, and later worker packages own execution. |
| Business | Keep integration cost low. | A thin editor adapter can be replaced or reused without changing execution products. |
| Company / meta^10 | Preserve a buyer-auditable, transferable asset. | The small boundary and machine proof reduce hidden coupling, handoff cost, and diligence risk. |

## One-line readback

> Vim is a thin client of `hq`: completion and draft are read-only; explicit accept may append exactly one instruction row; Vim and `hq` do not execute workers or target adapters.

## Input contract

| Field | Required for | Meaning |
|---|---|---|
| `buffer` | complete, draft, accept | Current UTF-8 buffer passed without editor-specific meaning. |
| `cursor` | complete | Byte offset in `buffer`; omitted means end of buffer. |
| `schemaPath` | optional | JSONL schema/world input selected by the caller. |
| `queuePath` | accept-to-queue only | Explicit append destination. No implicit queue path is created. |
| `action` | all | Exactly one of `complete`, `draft`, or `accept`. |

The editor may retain display state, but that state is not part of the compiler contract and cannot authorize execution.

## Public CLI mapping

| Vim action | Canonical hq invocation | Durable side effect |
|---|---|---|
| complete | `hq --schema <path> --complete <buffer> --cursor <byte>` | None. |
| draft | `hq --schema <path> --draft <buffer>` | None. |
| accept without queue | `hq --schema <path> --accept <buffer>` | None. The accepted draft is returned only. |
| accept to queue | `hq --schema <path> --accept <buffer> --queue <path>` | Exactly one append-only JSONL row. |

`--schema` is omitted when the built-in world is intended. Vim must not implement a second completion engine or reinterpret the returned suggestion meaning.

## Output contract

| Action | Output |
|---|---|
| complete | JSON array of suggestions, including edit data and `compileDraft`. |
| draft | One compile-draft JSON object. |
| accept | The accepted compile-draft JSON object; when `--queue` is present, the appended row must be byte-level JSON-equivalent to this object. |

The current compiler shape uses `kind = accepted.instruction` and `queue = instruction.jsonl`. This is an accepted **compiler draft / queue intent** mapping. It is not worker execution, admission, accepted-ledger authority, or canonical state.

## Error contract

- A non-zero `hq` exit is returned to the editor as an error.
- The editor keeps the user buffer and does not fall back to shell or adapter execution.
- Invalid JSON output is treated as a compiler error and is not appended by the editor.
- Completion or draft failure never causes a queue write.
- Accept failure never starts a downstream worker.

## Forbidden responsibilities

Vim and the `hq` compiler boundary must not:

- invoke Herdr, Codex, Claude, shell, PTY, or any other execution target;
- dispatch a worker or infer a target adapter;
- append during completion, candidate display, preview, or draft;
- append more than the one explicitly accepted candidate;
- create an implicit queue when `--queue` is absent;
- treat queue intent as accepted-ledger or source-of-truth authority.

## Machine proof

The checked-in contract is `spec/fixtures/vim-hq.contract.jsonl`.

`scripts/vim-hq-contract-proof.py` runs the official `cmd/hq` binary and proves:

1. buffer/cursor completion returns the expected candidate and compile target;
2. completion writes zero rows even when a queue path is supplied;
3. draft writes zero rows even when a queue path is supplied;
4. accept without `--queue` creates no implicit queue;
5. accept with `--queue` appends exactly one row equal to the accepted output;
6. all proof paths are independent of worker and adapter implementations.

The Linux and Windows official hq workflow uploads the generated proof JSON as review evidence. Generated proof output is evidence, not source authority.

## Issue closure mapping

| Issue | Closure evidence |
|---|---|
| #72 | This document fixes inputs, actions, outputs, errors, side effects, and forbidden ownership. |
| #73 | The `complete` fixture and proof use the public CLI with Vim-style buffer/cursor input and prove zero writes. |
| #74 | The `draft` and `accept` fixtures prove preview is read-only and explicit accept appends exactly one row. |
