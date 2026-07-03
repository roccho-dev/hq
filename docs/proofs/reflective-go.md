# Go terminal runtime proof notes

This document preserves the earlier reflective implementation proof while removing proof notes from the repository root.

## Current product readback

`hq` is the Go terminal runtime direction for JSONL-aware autocomplete compilation.

Current official command path:

```bash
go test ./...
go build -o dist/hq-linux-amd64 ./cmd/hq
GOOS=windows GOARCH=amd64 go build -o dist/hq-windows-amd64.exe ./cmd/hq
```

## Earlier proof source

The earlier root `PROOF.md` recorded a proof-era command path and binary names:

```text
go test ./...
go build -o dist/hq-reflective-linux-amd64 ./cmd/hq-reflective
GOOS=windows GOARCH=amd64 go build -o dist/hq-reflective-windows-amd64.exe ./cmd/hq-reflective
go list -m all
```

That evidence is retained as historical proof only. It is not the product identity and it is not source authority.

## Current CI proof names

Expected reviewable proof workflows:

| Workflow | Purpose |
|---|---|
| `CI` | Python reference tooling preview and baseline tests |
| `Protocol contract` | Go and Python reference tooling consume the same protocol fixture |
| `Interactive tab proof` | Linux and Windows literal Tab terminal proof |
| `Windows reflective proof` | Windows build, smoke, and contact-sheet proof |

Expected proof artifacts after this cleanup:

| Artifact | Meaning |
|---|---|
| `hq-interactive-tab-proof-linux` | Linux literal Tab proof transcript and binary |
| `hq-interactive-tab-proof-windows` | Windows literal Tab proof transcript and binary |
| `hq-windows-actual-run-proof` | Windows build/smoke/contact-sheet proof |
| `hq-protocol-contract-fixtures` | Protocol fixture used by Go and Python reference tooling |

## Boundary

- Proof artifacts are evidence, not source authority.
- Generated binaries belong in CI or release artifacts.
- Transitional committed `dist/` artifacts are governed by `dist/MANIFEST.md` until replaced or removed.
