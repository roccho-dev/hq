# Selected world identity and compile provenance

Issue: #113

## Purpose lineage

| Generation | Contribution |
|---:|---|
| G0 | Identify the exact world and command contract used for one accepted instruction. |
| G1 | Stop LSP and worker processes from silently using different world revisions. |
| G2 | Reject collisions and provenance mismatch before provider effect. |
| G3 | Give #116 deterministic world/index identity and #117 non-inferred history provenance. |
| G4 | Keep the editor, worker queue, and provider contracts independent of world composition. |
| G5 | Preserve legacy logs while making new stronger claims explicit. |
| G6 | Keep activation to atomic profile/world replacement plus restart. |
| G7 | Make semantic digests independent of JSONL row order and formatting. |
| G8 | Let a third party inspect one small versioned contract during transfer. |
| G9 | Allow vocabulary growth through data without growing runtime selection logic. |
| G10 | Reduce hidden deployment knowledge and diligence risk in the saleable product asset. |

## Fixed architecture

```text
active hq.profile.v1
  -> one absolute world_path
  -> optional legacy world, or one strict hq.world.v1 manifest
  -> normalized semantic world
  -> canonical sha256 digest
  -> LSP load at process start
  -> explicit submit
  -> accepted.instruction + optional hq.compile.provenance.v1
  -> worker loads active world
  -> exact provenance check before provider effect
```

A selected world is one immutable aggregate. v1 has no multi-world runtime, discovery, namespace/precedence engine, or hot reload.

`envs` owns generation, collision-free composition, placement, and atomic profile replacement. `hq` owns strict parsing, normalization, digesting, compile evidence, and fail-closed consumption.

## World record

```json
{"kind":"hq.world.v1","world_id":"example.production"}
```

A strict selected world contains exactly one such record. `world_id` is stable across compatible content revisions. The computed digest identifies exact normalized semantic content.

The digest excludes:

- source path;
- JSONL row order;
- whitespace;
- object-key order.

Declared array order remains semantic where it controls rendering or execution.

## Read-only selection observation

Issue #128 is the authority for query-independent deployment observation. The
strict loaders expose identity already computed by hq; these reports do not
select, compile, activate, accept, queue, or execute anything.

```text
hq world inspect --path <absolute-world-jsonl> --json
{"kind":"hq.selectedWorldInspection.v1","world":{"world_id":"example.production","digest":"sha256:..."}}

hq profile inspect --profile <name> --profile-root <absolute-root> --json
{"kind":"hq.profileSelectionInspection.v1","profile":"local","deployment_id":"deployment-1","world_path":"<absolute-world-jsonl>","world":{"world_id":"example.production","digest":"sha256:..."}}
```

Both commands emit one JSON line, use only explicit absolute regular-file
paths, invoke the strict selected-world loader, and write zero accepted, event,
or history rows.

The standard LSP `initialize` result reports the exact world loaded by that
process as `capabilities.experimental.hq` with kind
`hq.runtimeSelection.v1`, runtime `lsp`, profile, deployment ID, and world
reference. An identity-free legacy LSP world retains its existing completion
behavior but emits no selected-world claim and therefore cannot satisfy
deployment activation evidence. New managed workers write
`worker.heartbeat.v2` with required
`selected_world`; `hq-worker health` emits `hq.workerHealth.v2` and is ready
only when the fresh heartbeat profile, deployment, world, and ready state match
the current strict profile inspection. A v1 heartbeat remains readable only as
bounded compatibility/recovery evidence and can never prove readiness.

World/profile replacement does not hot-reload either process. Deployment must
restart the official LSP and managed worker, then compare their new in-process
reports. hq performs no process discovery or activation and does not interpret
an authoring ledger or release manifest.

## Command identity

Every command in a strict selected world contains:

```json
{
  "kind": "hq.command.v1",
  "command_id": "herdr.read",
  "command_version": "1",
  "name": "herdr.read"
}
```

- `command_id` is the stable machine identity;
- `command_version` is explicit compatibility identity;
- `name` remains the human/editor spelling;
- the computed command digest identifies exact normalized command content.

Duplicate command names, duplicate command IDs, missing half-identities, duplicate world records, and invalid identities fail before activation.

## Accepted compile provenance

New explicit submit under a strict selected world adds evidence beside the instruction:

```json
{
  "kind": "hq.compile.provenance.v1",
  "input_kind": "hq.command.v1",
  "world": {
    "world_id": "example.production",
    "digest": "sha256:..."
  },
  "command": {
    "command_id": "herdr.read",
    "command_version": "1",
    "name": "herdr.read",
    "digest": "sha256:..."
  },
  "instruction_digest": "sha256:..."
}
```

The instruction remains the only execution authority. Provenance is evidence of compilation and must never be reconstructed from instruction text.

Every explicit submit generates a fresh instruction ID and fresh `created_at` before the instruction digest is computed.

## Compatibility

| Case | Execution | Exact replay claim | Future #117 recall eligibility |
|---|---:|---:|---:|
| legacy accepted row without provenance | existing contract | no | no |
| exact world and command provenance | yes | yes | yes, after accepted-input and policy checks |
| world ID mismatch | no | no | no |
| exact world digest mismatch | no | no | possible only by recompile under #117, never direct execution |
| command ID/version/digest mismatch | no | no | no unless a later explicit migration contract permits it |
| instruction digest mismatch | no | no | no |
| malformed or ambiguous provenance | no | no | no |

A mismatch is represented as typed validation evidence and starts zero provider processes.

## Required-world support matrix

| Required class | Representation | Runtime owner | New subsystem required |
|---|---|---|---:|
| host capability commands | `hq.command.v1` lowering to existing `instruction.v1 target=host` | existing host adapter | no |
| Herdr/Codex/Claude tasks | command data lowering to existing agent targets | existing agent adapters | no |
| finite verified local tools | `hq.local-tool.v1` plus command data | existing local-tool preparer/runner | no |
| explicit-location remote tasks in #115 | command data lowering to an existing agent instruction | existing agent path | no |
| world-derived recall in #116 | read-only projection of the strict selected world | hq recall core | no runtime selection system |
| accepted-input recall in #117 | read-only projection of provenance-complete accepted evidence | hq recall core | no second history database |
| arbitrary external schema/plugin | unsupported until explicitly projected/versioned | none | not prebuilt |

## Rejection conditions

Reject any implementation that introduces:

- multiple simultaneously active worlds;
- PATH/current-directory/most-recent-file discovery;
- runtime namespace or precedence selection;
- hot reload in v1;
- source-path or row-order identity;
- text inference of command/world provenance;
- accepted evidence as execution meaning;
- direct execution after a provenance mismatch;
- a second queue, result, history, or provider contract;
- mandatory migration of legacy accepted rows into invented provenance.

## Operational activation

```text
produce complete world
  -> validate collision-free selected contract
  -> place immutable file
  -> replace active profile/world binding atomically
  -> restart LSP and managed worker
```

The first accepted instruction after restart carries the new exact world digest. An old accepted row observed by a worker using a different exact selected world fails closed before effect.
