# Review

Official product-runtime review starts from `cmd/hq`, the `hq-*` binaries, and the Official hq proof workflow. There is no second official `hq-reflective` command.

## Required readback

A reviewer must be able to say:

> `hq is meaning-free; JSONL carries meaning; a downstream worker executes.`

This means `hq` compiles and accepts instructions, but does not interpret target-specific execution meaning or launch a process. Vim remains a thin client of this same public contract. Worker input, validation, result, session, and status use the versioned contracts under `spec/`.

## Issues #32-#35 merge gate

| Gate | Required evidence |
|---|---|
| Boundary is explicit | README and `docs/boundary.md` list allowed and forbidden responsibilities. |
| Core is schema-independent | `internal/core/boundary_test.go` rejects concrete operation and target vocabulary. |
| Core invents no domain default | `internal/core/finalize_test.go` proves opaque instruction data is preserved. |
| Execution cannot creep into compiler packages | `internal/hq/execution_boundary_test.go` scans `internal/core`, `internal/hq`, and `cmd/hq`. |
| Equivalent escape routes are blocked | Raw syscall, Windows CreateProcess, cgo, plugin, linkname, PTY, and named adapters are negative fixtures. |
| Guard fails closed | Negative fixtures are rejected by the same execution-boundary detector. |
| Preview is side-effect free | `complete`, `context`, and `draft` produce no queue file. |
| Acceptance is bounded | One `accept --queue` produces exactly one row matching returned output. |
| Queue is explicit | `accept` without `--queue` adds no durable row. |
| No hidden target execution | Fake Herdr/Codex/Claude/shell executables first on `PATH` are never called. |
| Worker SSOT survives | `instruction.v1`, `validation.v1`, `result.v1`, `session.v1`, and status negative fixtures all pass. |
| Compiler/runtime rows are not conflated | `accepted.instruction` is compiler intent; canonical worker rows remain downstream contracts. |
| Vim contract survives | Linux and Windows Vim proof still show read-only complete/draft and one explicit append. |
| Existing product behavior survives | `go test ./...`, protocol contract tests, Linux build, Windows build, and literal Tab proof pass. |
| No authority overclaim | Generated binaries and workflow artifacts remain evidence only. |

## Review sources

- `README.md`
- `docs/boundary.md`
- `docs/architecture/core-port-adapter-boundary.md`
- `docs/evidence/hq-meaning-free-boundary.md`
- `docs/vim-to-hq-contract.md`
- `spec/instruction/v1.md`
- `spec/validation/v1.md`
- `spec/result/v1.md`
- `spec/session/v1.md`
- `spec/status/v1.md`
- `internal/core/boundary_test.go`
- `internal/core/finalize_test.go`
- `internal/hq/execution_boundary_test.go`
- `scripts/check.sh`
- Official hq proof workflow status and artifacts

A green workflow without the required readback is insufficient. Matching prose without the mechanical and functional gates is also insufficient.
