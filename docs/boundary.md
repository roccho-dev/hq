# hq boundary

## One-line readback

`hq is meaning-free; JSONL carries meaning; a downstream worker executes.`

`hq` may compile cursor and buffer context into suggestions and drafts, accept one human-selected draft, and append that accepted instruction as one JSONL row. It must not launch, dispatch, supervise, retry, observe, or attach to an external process.

## Purpose lineage

| Generation | Purpose | Direct contribution of this boundary |
|---:|---|---|
| G0 scope | Keep `complete`, `context`, `draft`, `accept`, and append-only queue write deterministic. | Defines the only allowed side effect: one accepted row may be appended. |
| G1 component | Keep core compilation independent of business and tool vocabulary. | Concrete words live in JSONL schema adapters, not `internal/core`. |
| G2 runtime | Keep process execution outside `hq`. | Worker and target adapters own execution after queue validation. |
| G3 system | Make JSONL the durable handoff between compiler and runtime. | Editor/compiler and worker can change independently. |
| G4 workflow | Prevent preview or candidate display from becoming hidden execution. | `complete`, `context`, and `draft` never append or execute. |
| G5 organization | Give editor, compiler, worker, and adapter owners non-overlapping responsibility. | Review and incident ownership stay clear. |
| G6 scale | Allow new targets without changing compiler code. | Adding a worker adapter does not expand `hq` core. |
| G7 operations | Keep execution policy, retry, sessions, and observation in one downstream runtime. | Operational behavior is not fragmented across editor and compiler. |
| G8 asset | Produce machine-checkable boundary evidence. | Static negative fixtures and functional append-only proof run in CI. |
| G9 due diligence | Let a reviewer verify the boundary without chat history. | Docs, tests, and CI all state and enforce the same rule. |
| G10 highest | Increase transferability and sale value of the company. | A small replaceable compiler and independently replaceable runtimes reduce buyer risk. |

## Responsibility table

| Area | Allowed in `hq` | Forbidden in `hq` |
|---|---|---|
| Input | JSONL world, buffer, cursor, schema path | Live target discovery or target sessions |
| Pure behavior | Analyze context, rank suggestions, build `compileDraft` | Execution policy, retry policy, session state |
| Human action | Accept one selected draft | Auto-accept or auto-dispatch |
| Durable effect | Append exactly one accepted compiler-envelope row when a queue path is explicitly supplied | Launch shell, Herdr, Codex, Claude, PTY, or any other process |
| Vocabulary | Schema-independent protocol types and adapter-provided data | Hard-coded target adapter identifiers in core/compiler packages |
| Observation | Return compiler output to stdout | Read target logs, tail runs, attach to sessions |

## Compiler-to-worker handoff

| Row | Owner | Meaning |
|---|---|---|
| `accepted.instruction` | `hq` compiler surface | Human acceptance envelope and queue intent only. |
| `instruction.v1` | worker contract | Canonical validated worker input. |
| `validation.v1` | worker validation | Evidence that input was rejected before a run existed. |
| `result.v1` | worker runtime | The only durable run-event/output/error contract. |
| `session.v1` | worker projection | Rebuildable run/session view, not authority. |

`hq` does not write result/session rows and does not decide that its own envelope is executable. Validation/mapping into `instruction.v1` belongs downstream.

## Package boundary

| Path | Responsibility |
|---|---|
| `internal/core` | Schema-independent compilation mechanics and protocol types. |
| `internal/boundary` | Abstract read/write ports. |
| `internal/adapter/current` | Current proof-era JSONL vocabulary and file translation. |
| `internal/hq` | Compatibility surface over core types and adapter-owned world data; no process execution. |
| `cmd/hq` | CLI and interactive compiler surface; accepted-row append only. |
| worker contract/core packages | Validation, policy, canonical results, session projection, and dispatch preparation. |
| target adapters | Target-specific effects after worker policy allows dispatch. |

## Mechanical proof

- `internal/core/boundary_test.go` rejects concrete schema and adapter vocabulary in core source.
- `internal/core/finalize_test.go` proves core preserves opaque adapter data and invents no operation.
- `internal/hq/execution_boundary_test.go` scans `internal/core`, `internal/hq`, and `cmd/hq` for process-launch APIs, raw syscall/cgo/plugin/linkname escapes, PTY dependencies, and named execution adapters. Negative fixtures prove the guard itself fails closed.
- `scripts/check.sh` runs Go plus worker protocol contract tests, proves `complete`, `context`, and `draft` do not append; one `accept --queue` appends exactly one row; `accept` without `--queue` does not add another row; fake target executables placed first on `PATH` are never invoked; and the Vim contract remains green.

## Generated output boundary

Build outputs, terminal transcripts, screenshots, Vim proof JSON, and workflow artifacts are review evidence only. They are not schema authority, queue authority, worker authority, or execution authority.
