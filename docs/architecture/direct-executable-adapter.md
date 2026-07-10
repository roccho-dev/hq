# Direct executable adapter

## Purpose

G lane supplies the first concrete process adapter without turning `hq` or the worker contract into a shell runtime.

```text
canonical instruction.v1 target=sh
  -> common worker validation
  -> workspace and timeout policy
  -> exact digest-bound approval
  -> project-local single-writer claim
  -> directexec adapter
  -> transient stdout / stderr / completion / error
  -> worker-owned result.v1
  -> rebuildable session / ledger / show / tail
```

The target key remains `sh` for v1 compatibility. Its executable meaning is direct argv only.

## Ownership

| component | owns | must not own |
|---|---|---|
| `hq` | completion, draft, explicit accepted envelope append | process launch or target meaning |
| worker | validation, cwd, timeout, approval, claim, run identity, sequence, redaction, append, projection | shell syntax or provider CLI details |
| directexec adapter | one direct child process and transient captured output | durable identity, lifecycle, approval, redaction, retry, or fallback |
| `result.v1` | durable process output and terminal outcome | execution authority |
| session/ledger/detail | rebuildable read models | authority or mutation |

## Input meaning

The adapter receives the existing canonical payload:

```json
{"argv":["/explicit/path/program","arg one",";","|"],"cwd":"."}
```

Rules:

- `argv[0]` is an absolute or explicit relative path;
- a bare executable name is blocked because it would require ambient PATH lookup;
- `argv[1:]` is passed as exact argument values;
- shell punctuation, quoting characters, wildcard characters, `%NAME%`, and `$NAME` are ordinary strings;
- no `/bin/sh`, `bash`, `cmd.exe`, PowerShell, shell flag, or string evaluation is inserted;
- the child uses the worker-approved effective cwd;
- ambient environment is not inherited;
- interactive stdin, PTY, and TUI behavior are outside v1.

## Lifecycle

```text
policy evidence
  -> accepted
  -> started
  -> zero or more stdout/stderr rows
  -> exactly one completed | failed | timeout | cancelled row
```

The adapter cannot set event id, run id, instruction id, sequence, timestamp, kind, or status. Existing worker mapping derives all durable fields.

| adapter outcome | canonical terminal result |
|---|---|
| exit status 0 | `completed` with deterministic final text |
| non-zero exit | `failed/process_exit_nonzero` with numeric status |
| process start failure | `failed/process_start_failed` |
| context deadline | `timeout/deadline_exceeded` |
| explicit cancellation | `cancelled/cancel_requested` |
| bare executable name | `blocked/executable_path_required` |

Output written before timeout, cancellation, or non-zero exit remains transiently emitted, then passes through common redaction before durable append.

## Recovery composition

The merged installed-runtime recovery contract remains unchanged:

- a terminal direct run is rejected as a normal duplicate;
- a direct run with durable `started` but no terminal evidence cannot prove an idempotent provider contract and therefore becomes `reconcile_required`;
- only exact provider-bound adapters with retained idempotency evidence may resume automatically.

G does not weaken or bypass the provider recovery rules added by the installed runtime.

## Safety boundary

This adapter proves a small execution boundary, not a complete sandbox.

It guarantees:

- no implicit shell;
- no PATH fallback;
- no ambient environment inheritance;
- validation and exact approval before process start;
- one normal execution for one instruction id;
- direct-child timeout/cancellation;
- one canonical durable lifecycle;
- restart-safe readback.

It does not guarantee:

- executable allowlisting;
- filesystem or network confinement;
- descendant process-tree termination;
- PTY/TUI control;
- distributed exactly-once effects;
- full DLP or secret-manager behavior.

## Evidence

| claim | executable evidence |
|---|---|
| metacharacters remain literal | helper argument JSON in Linux and Windows proof |
| no ambient environment | directexec unit test inspects a parent-only secret key |
| no PATH lookup | bare-name negative test returns `executable_path_required` |
| approval before start | sentinel remains absent for missing approval |
| one execution | sentinel contains one line after approved run and duplicate read |
| output and final readback | canonical events plus list/show/tail artifacts |
| non-zero exit | runner and adapter tests require `process_exit_nonzero` |
| timeout/cancel | actual helper child tests on each workflow host |
| durable secret absence | proof scans emitted and durable JSON for known plaintext |
| provider recovery retained | existing installed-runtime recovery tests remain in full CI |

Primary proof surfaces:

- `internal/worker/directexec/directexec_test.go`
- `internal/worker/directexec_runner_test.go`
- `scripts/check-directexec.sh`
- `.github/workflows/directexec-proof.yml`

## Reviewer sentence

> Canonical `target=sh` means direct explicit-path argv execution, not shell language. The common worker owns every safety, recovery, and durable lifecycle decision; the adapter owns only one direct child and transient output.
