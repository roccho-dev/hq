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
| Correctness authority | `spec/fixtures/` protocol fixtures |
| Official runtime direction | Go terminal runtime |
| Python | Tooling only under `tools/hq_reference`: preview rendering, fixture checks, migration helpers, reference checks |
| Proof | Linux and Windows terminal Tab proof, stored as CI artifacts and documented under `docs/proofs/` |
| Generated binaries | CI or release artifacts, not source authority; transitional committed artifacts are governed by `dist/MANIFEST.md` |

The repository should read as a product with proofs, not as a pile of POCs.

## Current cleanup path

Issue #13 owns the cleanup from the current proof-heavy tree into this final shape:

```text
hq
  protocol-first
  Go runtime
  Python tooling
  Linux/Windows terminal proof
```

The first cleanup slice fixed the product identity and decision record. The second slice made `cmd/hq` and the Go module name the official product runtime shape. The third slice moved Python into `tools/hq_reference`. The fourth slice added `spec/fixtures` and protocol-contract CI. This slice adds artifact-source boundaries for `dist/`. Proof-doc relocation remains a separate #13 slice.

## What hq must do

| Behavior | Expected result |
|---|---|
| User starts a JSONL object | Required key suggestions appear |
| User types a partial key or value | Context-valid low-noise candidates appear |
| Candidate is displayed | Candidate includes edit intent and `compileDraft` |
| Candidate is accepted | Only that accepted candidate becomes an instruction |
| Candidate is not accepted | Nothing is written to the queue |
| Schema changes | Suggestions change without hardcoding business keys in product code |
| Linux/Windows Tab proof runs | Literal Tab operation remains guarded by CI evidence |

## Runtime and proof status

The official Go command path is `cmd/hq`, and the module name is `hq`.

Python reference tooling lives under `tools/hq_reference` and is invoked as:

```bash
PYTHONPATH=. python3 -m tools.hq_reference demo-autocomplete
```

Protocol fixtures live under `spec/fixtures` and are checked by:

```bash
go test ./internal/hq -run TestProtocolFixtureContract -v
PYTHONPATH=. python3 -m unittest tests/test_python_reference_protocol_fixture.py
```

`dist/` is not source authority. New generated binaries are ignored by default. Any transitional committed proof artifact in `dist/` must be explained by `dist/MANIFEST.md` until PR6 replaces or moves the proof evidence.

During the remaining #13 cleanup:

1. `docs/proofs/` keeps proof knowledge outside the product root.
2. Legacy proof paths may exist only as compatibility or evidence until the proof-doc/artifact cleanup slice removes or explains them.

## Reviewer readback

A reviewer should be able to say:

> `hq` is a Go terminal runtime for JSONL-aware autocomplete compilation. Its correctness is defined by protocol fixtures. Python is tooling. Linux and Windows terminal Tab UX are protected by interactive proof CI. Generated binaries and proof captures are artifacts, not source authority.

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
