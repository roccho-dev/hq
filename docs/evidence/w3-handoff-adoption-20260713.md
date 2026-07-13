# W3 handoff adoption record — 2026-07-13

## Ownership

```text
local
└─ frozen; handoff producer complete

W3
├─ PR #119 finite generic local-tool core owner
├─ Explorer change -> existing host.open adapter lane
└─ WSLC change -> data-only local-tool consumer after #113/#122
```

W3 does not apply the handoff as one patch and does not permit another generic local-tool implementation.

The handoff path named by the producer was:

```text
/home/nixos/handoffs/hq-pr119-w3-20260713
```

That filesystem was not mounted in the GitHub-connected execution environment used for this integration. Therefore this record does **not** claim a fresh local `SHA256SUMS` or `git apply --check` run over that directory. No handoff patch was applied wholesale. Review used the corresponding GitHub PR diffs and rebuilt accepted changes on the current `proposals` head.

## Hunk disposition

| Source area | Disposition | Current owner/result | Reason |
|---|---|---|---|
| PR #119 generic local-tool schema, preparer, runner, result integration | already adopted | merged PR #119 | Canonical generic core; any duplicate implementation is rejected. |
| Explorer `host.open` Run-to-Start handoff fix | adopted after rebuild | merged PR #125; old PR #120 closed | Non-duplicate bug fix in the existing host adapter. Kept outside local-tool core. |
| Explorer deterministic adapter negatives | adopted after repair | merged PR #125 | Preserves late non-zero exit, invalid cwd, poisoned PATH, spaces, and pre-effect cancellation evidence. |
| Explorer helper-evidence read race | adopted as separate test-only fix | merged PR #127 | Complete JSON/text must be observed before asserting handoff evidence. Kept outside the WSLC data-only lane. |
| Explorer native pc7337 evidence | deferred | W1 release gate | Mutable host is owned by W1. Historical evidence is not the exact-head merge authority. |
| WSLC `hq.local-tool.v1` data row | adopted | current data-only WSLC PR | Second unrelated consumer proves the merged generic core accepts new tools through world data. |
| WSLC `hq.command.v1` row without stable identity | rejected and replaced | `command_id=wslc.version`, `command_version=1` | #113/#122 requires explicit stable command identity/version in selected worlds. |
| WSLC strict loader and provider-data absence tests | adopted after rebuild | current data-only WSLC PR | Negative tests are non-duplicate and guard the data/core boundary. |
| WSLC completion/lowering test | adopted after rebuild | current data-only WSLC PR | Proves the queue contains only tool/version/action/input and no provider or envs data. |
| WSLC generic compiler/worker changes | rejected | zero production hunks | PR #119 already owns the generic path; WSLC must remain data-only. |
| WSLC Program Files binding, digest, known-folder resolution | not owned here | envs #31/#32 | Provider placement and verified binding remain envs responsibilities. |
| WSLC native pc7337 execution proof | deferred | W1 release gate | No new mutable-host run is allowed while W1 owns pc7337. |
| Handoff-wide patch application | rejected | none | Would mix unrelated adapter/data changes and could reintroduce duplicate generic core. |

## Final acceptance boundary

The WSLC repository diff is allowed to contain only:

- selected-world JSONL data;
- strict data-boundary tests;
- completion/lowering negative proof;
- this adoption record.

It must contain zero changes to compiler mechanics, worker lifecycle, adapter registry, local-tool preparer, execution, result contracts, Vimscript, envs binding semantics, or host-open code.
