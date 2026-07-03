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
| Official runtime direction | Go terminal runtime |
| Python | Tooling only: preview rendering, fixture checks, migration helpers, reference checks |
| Proof | Linux and Windows terminal Tab proof, stored as CI artifacts and documented under `docs/proofs/` |
| Generated binaries | CI or release artifacts, not source authority |

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

The first cleanup slice fixed the product identity and decision record. This rename slice makes `cmd/hq` and the Go module name the official product runtime shape. Python movement, protocol fixtures, binary cleanup, and proof-doc relocation remain separate #13 slices.

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

The official Go command path is now `cmd/hq`, and the module name is `hq`.

During the remaining #13 cleanup:

1. Python is kept only when it serves tooling or reference checks.
2. `spec/` becomes the correctness authority for protocol fixtures.
3. `docs/proofs/` keeps proof knowledge outside the product root.
4. Generated binaries move to CI/release artifacts unless explicitly justified.
5. Legacy proof paths may exist only as compatibility or evidence until the proof-doc/artifact cleanup slice removes or explains them.

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
