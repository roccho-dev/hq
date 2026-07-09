# hq

`hq` is a JSONL-aware autocomplete compiler.

It reads a JSONL world and the user's current cursor context, returns compile-ready suggestions, accepts one human-selected suggestion, and writes the accepted instruction as append-only JSONL.

```text
JsonlWorld + CursorContext
  -> Suggestion[] with compileDraft
  -> Acceptance
  -> Instruction JSONL row
```

## Product identity

| Area | Role |
|---|---|
| Product | `hq`, a terminal runtime for JSONL-aware autocomplete compilation |
| Protocol | JSONL world, cursor context, suggestion, compileDraft, acceptance, instruction |
| Correctness authority | `spec/fixtures/` contract rows and CI checks |
| Official runtime direction | Go terminal runtime |
| Python | Tooling only: preview rendering, fixture checks, migration helpers, reference checks |
| Evidence | CI status, workflow artifacts, `docs/evidence/`, `docs/review.md`, and `PROOF.md` pointer note |
| Generated outputs | Review evidence only, not source authority |

The repository should read as a product with evidence, not as a pile of POCs.

## Completed #13 cleanup path

Issue #13 normalizes this repo into:

```text
hq
  protocol-first
  Go runtime
  Python tooling
  Linux/Windows terminal checks
```

The cleanup path has established product identity, Go module/command ownership, Python tooling metadata, protocol fixture rows, generated-output boundary notes, and review/evidence notes.

## Worker boundary contracts

`hq` still stops at append-only instruction output. Worker execution uses versioned JSONL contracts:

| Contract | Role |
|---|---|
| [`instruction.v1`](spec/instruction/v1.md) | Validated worker input; `hq` may append it but never executes it. |
| [`result.v1`](spec/result/v1.md) | Append-only run events, output, final answer, and errors. |
| [`session.v1`](spec/session/v1.md) | Rebuildable list/show projection, not a second authority. |
| [status taxonomy v1](spec/status/v1.md) | Shared queued/running/terminal states and transitions. |

Canonical, invalid, run, projection, and transition evidence lives under `spec/fixtures/` and is executed by `python3 -m unittest discover -s tests`.

## What hq must do

| Behavior | Expected result |
|---|---|
| User starts a JSONL object | Required key suggestions appear |
| User types a partial key or value | Context-valid low-noise candidates appear |
| Candidate is displayed | Candidate includes edit intent and `compileDraft` |
| Candidate is accepted | Only that accepted candidate becomes an instruction |
| Candidate is not accepted | Nothing is written to the queue |
| Schema changes | Suggestions change without hardcoding business keys in product code |
| Linux/Windows terminal checks run | Literal Tab operation remains guarded by CI evidence |

## Runtime and evidence status

The official Go command path is `cmd/hq`, and the module name is `hq`.

Python is labeled as tooling by `pyproject.toml` and `tools/README.md`.

Protocol contract rows live under `spec/fixtures/` and are checked by CI.

Generated outputs are review evidence only. See `docs/boundary.md`.

Review notes live under `docs/review.md` and `docs/evidence/README.md`. Root `PROOF.md` is now only a pointer note.

## Reviewer readback

A reviewer should be able to say:

> `hq` is a Go terminal runtime for JSONL-aware autocomplete compilation. Its correctness is defined by protocol fixtures. Python is tooling. Linux and Windows terminal UX are protected by CI evidence. Generated outputs are evidence, not source authority.

If the repo no longer supports that sentence, the cleanup is not complete.

## Build

```bash
go test ./...
go build -o dist/hq-linux-amd64 ./cmd/hq
GOOS=windows GOARCH=amd64 go build -o dist/hq-windows-amd64.exe ./cmd/hq
```

Or:

```bash
./scripts/check.sh
```
