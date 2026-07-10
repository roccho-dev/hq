# M cross-lane integration evidence

Closes #100, #101, and #102 when the final PR head passes all repository checks.

## Scope-to-highest-purpose

| generation | purpose | evidence |
|---:|---|---|
| G0 | Connect actual compiler output to the worker. | Built `cmd/hq` output is consumed by built `cmd/hq-worker`. |
| G1 | Preserve one canonical instruction contract. | Inner instruction equality and existing `instruction.v1` validation. |
| G2 | Make approval and redaction mandatory. | Common Runner call-count and durable secret-absence tests. |
| G3 | Prevent concurrent local ownership. | Atomic two-process claim proof on Linux and Windows. |
| G4 | Preserve one lifecycle authority. | Policy rows are evidence only; `result.v1` remains projection input. |
| G5 | Make restart/readback independent of terminal state. | Existing K list/show/tail works with the integrated event file. |
| G6-G10 | Reduce operating cost, key-person risk, DD uncertainty, and transfer cost. | Versioned rows, exact boundaries, cross-OS artifacts, and destructive tests. |

## Issue map

| issue | implementation | decisive evidence |
|---:|---|---|
| #100 | `internal/workeraccept` plus `--input-format accepted.instruction` | actual `cmd/hq --accept --queue` binary proof and exact inner equality |
| #101 | `internal/worker.Runner`, approval store, durable redaction boundary | missing/stale approval zero calls; approved fake adapter once; durable secret absence |
| #102 | `internal/workerclaim` plus worker CLI acquisition/recovery | two-process exclusion; exact stale recovery audit; normal release |

## Merge gate

- full repository CI succeeds on the final head;
- worker-core Linux and Windows jobs succeed;
- actual hq accepted-output proof succeeds on Linux and Windows;
- no unresolved review thread remains;
- branch is not behind `proposals`;
- the diff adds no concrete target adapter and no competing durable lifecycle contract.

## Parent relation

M completion removes the cross-lane blockers for G-J and L. It does not close #99 by itself; the parent still requires concrete adapters and final integrated proof.
