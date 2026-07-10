# hq

`hq` is a meaning-free JSONL autocomplete compiler and human-acceptance surface.

It reads an adapter-provided JSONL world plus the user's cursor context, returns compile-ready suggestions, accepts one human-selected draft, and may append that accepted instruction as one JSONL row. It does not execute the instruction.

> `hq is meaning-free; JSONL carries meaning; a downstream worker executes.`

```text
JsonlWorld + CursorContext
  -> Suggestion[] with compileDraft
  -> explicit human Acceptance
  -> accepted.instruction compiler envelope
  -> downstream validation / instruction.v1
  -> worker outside hq
```

## Product identity

| Area | Role |
|---|---|
| Product | `hq`, a terminal compiler and acceptance surface for JSONL instructions |
| Protocol | JSONL world, cursor context, suggestion, compileDraft, acceptance, instruction |
| Meaning owner | Adapter-provided JSONL schema and data |
| Execution owner | A downstream worker and its target adapters, outside `hq` |
| Correctness authority | `spec/fixtures/` contract rows and CI checks |
| Official runtime | Go command at `cmd/hq` |
| Python | Tooling only: preview rendering, fixture checks, migration helpers, reference checks |
| Evidence | CI status, workflow artifacts, `docs/evidence/`, and `docs/review.md` |
| Generated outputs | Review evidence only, not source or execution authority |

The repository should read as a small product with enforceable boundaries, not as a process runner or a collection of target-specific integrations.

## Allowed and forbidden responsibilities

| `hq` may | `hq` must not |
|---|---|
| Analyze buffer and cursor context | Launch shell, Herdr, Codex, Claude, PTY, or another process |
| Rank context-valid suggestions | Dispatch to named execution adapters |
| Build and display `compileDraft` | Decide execution policy, approval, retry, or cancellation |
| Accept one human-selected draft | Own run/session registries or target logs |
| Append exactly one accepted row when `--queue` is explicitly supplied | Treat preview, completion, or draft generation as acceptance |

Detailed ownership and proof are in [`docs/boundary.md`](docs/boundary.md) and [`docs/architecture/core-port-adapter-boundary.md`](docs/architecture/core-port-adapter-boundary.md).

## Worker boundary contracts

`hq` stops at compiler acceptance and explicit append. Worker processing uses versioned JSONL contracts downstream:

| Contract | Role |
|---|---|
| `accepted.instruction` | Current compiler acceptance envelope emitted by `cmd/hq`; queue intent only, never execution authority. |
| [`instruction.v1`](spec/instruction/v1.md) | Canonical validated worker input after downstream validation/mapping. |
| [`validation.v1`](spec/validation/v1.md) | Append-only rejection evidence for input that cannot become a valid instruction. |
| [`result.v1`](spec/result/v1.md) | Canonical append-only run events, output, final answer, and errors. |
| [`session.v1`](spec/session/v1.md) | Rebuildable list/show projection, not a second authority. |
| [status taxonomy v1](spec/status/v1.md) | Shared queued/running/terminal states and transitions. |

The compiler never writes `result.v1` or `session.v1`, and it never treats its acceptance envelope as a validated or executable worker row. Canonical, invalid, run, projection, and transition evidence lives under `spec/fixtures/` and is executed by the protocol contract tests.

## Worker observation

`cmd/hq-worker` exposes read-only observation over canonical durable rows:

| Command | Result |
|---|---|
| `hq-worker list` | Recent shell, Herdr, Codex, and Claude runs from one rebuildable ledger. |
| `hq-worker show --run <id>` | Request summary, lifecycle events, final answer/path, error, and native-session hint for one run. |
| `hq-worker tail --run <id>` | Current and newly appended canonical `result.v1` rows until terminal state. |

These commands never dispatch, approve, retry, claim, or append lifecycle evidence. Their JSON outputs are disposable read models, not a second SSOT. See [`docs/architecture/worker-observation.md`](docs/architecture/worker-observation.md) and [`docs/worker-retry.md`](docs/worker-retry.md).

## Expected behavior

| Behavior | Expected result |
|---|---|
| User starts a JSONL object | Required key suggestions appear |
| User types a partial key or value | Context-valid low-noise candidates appear |
| Candidate is displayed | Candidate includes edit intent and `compileDraft` |
| `--complete`, `--context`, or `--draft` runs | Nothing is appended and no external process starts |
| Candidate is accepted without `--queue` | Accepted instruction is returned; no durable row is added |
| Candidate is accepted with `--queue` | Exactly one accepted compiler-envelope row is appended |
| Schema data changes | Suggestions change without hard-coding business or target words in core |
| Linux/Windows terminal checks run | Literal Tab operation and compiler boundary remain guarded by CI evidence |

## Vim boundary

Vim is a thin client of the public `cmd/hq` contract. It may pass buffer, cursor, schema input, and an explicit queue path; display completion or draft output; and accept one candidate. Completion and draft are read-only. Accept without `--queue` is non-durable. Accept with `--queue` appends exactly one row. Neither Vim nor `hq` starts workers or target adapters.

See [`docs/vim-to-hq-contract.md`](docs/vim-to-hq-contract.md) and `spec/fixtures/vim-hq.contract.jsonl`. The same contract proof runs against the official Linux and Windows binaries.

## Package ownership

| Path | Responsibility |
|---|---|
| `internal/core` | Schema-independent compilation mechanics and protocol types |
| `internal/boundary` | Abstract read/write ports |
| `internal/adapter/current` | Current concrete JSONL vocabulary and file translation |
| `internal/hq` | Compatibility surface using core types and adapter-owned world data; no execution |
| `cmd/hq` | CLI, interactive compiler, explicit accepted-row append |
| `internal/worker` | Validation, policy preparation, canonical result events, deterministic session/ledger/detail reduction, and bounded durable-log follow |
| `cmd/hq-worker` | Worker processing plus read-only list/show/tail command surface |
| target adapters | Target-specific effects after worker validation and policy |
| `spec/fixtures`, tests, and docs | Contract and review evidence |

## Mechanical evidence

- `internal/core/boundary_test.go` blocks concrete schema and target vocabulary from entering core source.
- `internal/core/finalize_test.go` proves core preserves opaque adapter data and invents no default operation.
- `internal/hq/execution_boundary_test.go` blocks process-launch APIs, raw syscall/cgo/plugin/linkname escape routes, PTY dependencies, and named execution adapters from compiler packages. Negative fixtures prove the guard fails closed.
- `scripts/check.sh` runs Go tests plus all protocol contract tests, proves preview paths do not append, one accepted row appends exactly once, acceptance without a queue does not append, fake target executables on `PATH` are never invoked, and the Vim contract remains valid.
- `scripts/check-worker.sh` builds Linux/Windows worker binaries and proves dry-run, durable result/validation append, duplicate blocking, mixed-target list, one-run show, and terminal tail readback.
- The official Linux and Windows workflows run the Go tests and terminal/Vim proof paths.

## Build and check

```bash
go test ./...
go build -o dist/hq-linux-amd64 ./cmd/hq
GOOS=windows GOARCH=amd64 go build -o dist/hq-windows-amd64.exe ./cmd/hq
```

Or run the complete local proof:

```bash
./scripts/check.sh
```

## Reviewer readback

A reviewer should be able to say:

> `hq` is a Go terminal compiler for JSONL-aware autocomplete and explicit acceptance. Concrete meaning stays in adapter-provided JSONL. Completion and draft paths have no durable side effects. Acceptance can append one compiler envelope, but `hq` cannot validate it as executable or execute it; a downstream worker owns canonical instruction validation, execution, durable evidence, and rebuildable observation.

If the repository, tests, or CI no longer support that sentence, the boundary is broken.
