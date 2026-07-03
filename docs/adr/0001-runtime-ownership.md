# ADR 0001: Runtime ownership and protocol authority

## Status

Accepted for the #13 cleanup route.

## Context

The current repository contains valuable proof material, but its identity is mixed.

| Area | Current risk |
|---|---|
| README | Can read as `hq-reflective-poc` rather than `hq` |
| Go proof | Looks close to the product runtime but still carries proof names |
| Python package | Can be mistaken for a second product runtime |
| Proof artifacts | Are useful, but can dominate the product identity |
| Generated binaries | Can be mistaken for source authority |
| Protocol fixtures | Are not yet the visible correctness authority |

The cleanup must keep useful proof knowledge while making the product shape easy to understand.

## Decision

`hq` is protocol-first.

The protocol is the authority for correctness:

```text
JsonlWorld
CursorContext
Suggestion with compileDraft
Acceptance
Instruction
QueueAppend
```

Go is the official terminal runtime direction for the product.

Python is not a product runtime. Python may remain only as tooling, preview generation, fixture generation, migration support, compatibility checks, or reference-only tests.

Proofs are preserved, but they belong under `docs/proofs/` and CI artifacts, not as the root product identity.

Generated binaries and screenshots are artifacts. They should not be treated as source authority.

## Consequences

| Consequence | Result |
|---|---|
| Future runtime work | Must target the Go `hq` runtime unless a later ADR promotes another runtime |
| Future Python work | Must be labeled and structured as tools/reference/preview/migration only |
| Future correctness checks | Must converge on protocol fixtures under `spec/` |
| Future proof work | Must keep Linux/Windows terminal proof reviewable without making proof the product identity |
| Future artifacts | Must live in CI/release outputs unless a manifest explicitly justifies source-tree storage |

## Allowed additions

| Addition | Allowed when |
|---|---|
| New UI projection | It consumes protocol suggestions and does not become core authority |
| New tooling language | It stays under tools/reference/proof and consumes the protocol contract |
| New proof workflow | It proves a specific claim and records stable evidence names |
| New examples | They are business-key examples, not hardcoded product semantics |

## Disallowed additions

| Addition | Reason |
|---|---|
| A second official runtime without ADR | Reintroduces product authority split |
| Python package as product CLI authority | Conflicts with the Go-runtime decision |
| Proof binaries as source authority | Makes generated output look canonical |
| Hardcoded business keys as product semantics | Breaks JSONL-driven reuse |
| A fullscreen picker as core dependency | UI projection should not define core protocol |

## PR mapping

This ADR supports #13 PR1:

| PR | Scope | Closes acceptance criteria |
|---:|---|---|
| 1 | README + ADR product identity | 1, 5, 10, 11, 16 |

This ADR does not claim that the later slices are complete. Go rename, Python move, protocol fixtures, binary cleanup, and proof-doc relocation remain separate #13 PRs.
