# hq meaning-free boundary closure evidence

Closes #32, #33, #34, and #35.

## Scope-to-highest-purpose lineage

| Generation | Purpose | Contribution |
|---:|---|---|
| G0 scope | Bound `hq` to completion, draft, acceptance, and explicit append. | Direct: tests cover every allowed command path. |
| G1 component | Keep compilation independent of concrete business/tool vocabulary. | Direct: core vocabulary and behavior guards. |
| G2 runtime | Keep all process execution downstream. | Direct: compiler execution guard and fake-executable proof. |
| G3 system | Use JSONL as the only durable compiler-to-worker handoff. | Direct: one accepted row is the bounded interface. |
| G4 workflow | Prevent display and preview from causing hidden work. | Direct: no-append proof for complete/context/draft. |
| G5 organization | Separate editor/compiler/worker/adapter ownership. | Direct: responsibility and package maps. |
| G6 scale | Add or replace targets without expanding `hq` core. | Direct: named adapter terms fail the boundary check. |
| G7 operations | Centralize validation, policy, results, sessions, and recovery in a worker. | Indirect: `hq` is prevented from fragmenting those concerns. |
| G8 asset | Turn the boundary into repeatable evidence. | Direct: positive and negative CI fixtures. |
| G9 diligence | Let a third party verify claims from repository state. | Direct: one review index connects claims to checks. |
| G10 highest | Increase transferable company value and reduce buyer risk. | Indirect: small replaceable parts and enforceable ownership reduce integration and handoff cost. |

## Issue closure matrix

| Issue | Required result | Evidence |
|---:|---|---|
| #32 | `hq` is explicitly a meaning-free compiler; worker executes. | README, `docs/boundary.md`, architecture document, reviewer sentence. |
| #33 | External execution and named adapter creep are mechanically rejected. | `internal/hq/execution_boundary_test.go`, including process, syscall, Windows, cgo, plugin, linkname, PTY, and named-adapter negative fixtures. |
| #34 | Concrete operation/target vocabulary stays in schema adapters and data. | The old `queue.preview` core fallback is removed; package ownership, source guard, and `finalize_test.go` prove opaque behavior. |
| #35 | Acceptance appends one row only and never executes. | `scripts/check.sh` no-append, exact-one-row, no-queue, fake-executable, Vim contract, and worker contract assertions. |

## Contract alignment

| Surface | Canonical role |
|---|---|
| `accepted.instruction` | Compiler acceptance envelope and queue intent. |
| `instruction.v1` | Canonical validated worker input after downstream validation/mapping. |
| `validation.v1` | Rejection evidence before a valid run/session exists. |
| `result.v1` | The only durable worker run-event/output/error contract. |
| `session.v1` | Rebuildable worker projection. |

The compiler never emits runtime result/session evidence and never decides that its own acceptance envelope may dispatch.

## Positive proof

1. Existing valid completion still returns schema-provided candidates.
2. Existing context analysis still identifies cursor context.
3. Existing draft compilation still returns an accepted-instruction-shaped draft.
4. Core finalization preserves adapter-provided fields and invents no `op` value.
5. Explicit `accept --queue` appends one row.
6. The appended row is identical to the returned accepted instruction.
7. All current worker protocol contract tests remain green.
8. The official Linux and Windows command paths still build from `cmd/hq`.
9. The merged Vim contract remains read-only for complete/draft and exact-one-row for accept.

## Destructive cases and required outcome

| # | Attempted break | Required outcome |
|---:|---|---|
| 1 | Import `os/exec` or `execabs` into compiler packages. | Test fails. |
| 2 | Call `exec.Command`, `os.StartProcess`, or `syscall.StartProcess`. | Test fails. |
| 3 | Call Unix `Exec`/`ForkExec` or raw `syscall.Syscall`. | Test fails. |
| 4 | Call a Windows `CreateProcess*` API. | Test fails. |
| 5 | Use cgo `system`/`popen`, `go:linkname`, or runtime plugin loading. | Test fails. |
| 6 | Add a PTY execution dependency. | Test fails. |
| 7 | Hard-code `herdr`, `codex`, or `claude` in compiler implementation. | Test fails, including case variation. |
| 8 | Hard-code shell adapter names or executable paths. | Test fails. |
| 9 | Move `queue.create`, `queue.preview`, or another concrete operation into core. | Core boundary test fails. |
| 10 | Make core invent an `op` when finalizing an opaque instruction. | Core behavior test fails. |
| 11 | Run `--complete` with a queue path. | No queue file is created. |
| 12 | Run `--context` with a queue path. | No queue file is created. |
| 13 | Run `--draft` with a queue path. | No queue file is created. |
| 14 | Run `--accept` without a queue path. | Output is returned; no row is appended. |
| 15 | Run one `--accept --queue`. | Exactly one row is appended. |
| 16 | Put fake target executables first on `PATH`. | No executable is invoked. |
| 17 | Let Vim create an implicit queue or duplicate execution path. | Existing cross-platform Vim proof fails. |
| 18 | Remove or weaken worker contract validation. | Protocol contract tests fail. |
| 19 | Treat `accepted.instruction` as `result.v1` or session authority. | Docs/readback disagree; PR is not merge quality. |
| 20 | Add prose claiming `hq` owns retry/session/log observation. | Responsibility tables disagree; PR is not merge quality. |
| 21 | Keep green CI but remove negative fixtures. | Guard self-proof is missing; PR is not merge quality. |
| 22 | Treat workflow artifacts as source or execution authority. | Boundary documentation rejects the claim. |
| 23 | Preserve docs but reintroduce a concrete core fallback. | Source and behavior tests fail. |

## Authority and non-goals

This closure does not implement a target adapter, accepted-ledger admission, or remote authority. The compiler row remains queue intent; canonical validation, policy, execution, and evidence belong downstream.

## Merge-quality statement

The change is merge quality only when all repository checks pass on the current head, the branch is based on the current `proposals` head, and a reviewer can reconstruct the same boundary from README, architecture, tests, and functional proof without prior chat context.
