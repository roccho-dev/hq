# ops#83 host runtime PoC snapshot

## Status

This directory is a non-authority proposal snapshot. It does not replace the
official `cmd/hq`, alter the canonical hq boundary, or claim adoption.

## Provenance

| Field | Value |
|---|---|
| Source repository | `roccho-dev/ops` |
| Source PR | `roccho-dev/ops#83` |
| Source branch | `proposal/hq-lsp-jsonl-host-open-260710` |
| Source commit | `cfeec23d21633d3c78a4bee4427841b80d93fbef` |
| Source path | `packages/hq` |
| Snapshot mode | tracked files only |
| Excluded runtime data | `packages/hq/.tmp/**` |

The Go module path intentionally retains its source identity so this snapshot
can be compared byte-for-byte at the source-file level with the ops proposal.

## What this PoC proves

- an LSP process can derive completion items from a JSONL profile catalog;
- explicit `hq.submit` can append a semantic `host.open` queue row;
- queue and receipt files can remain outside Vim;
- a separate runner can invoke `explorer.exe` directly without a shell;
- repeated explicit submissions receive distinct queue IDs;
- Windows and non-Windows adapter behavior can fail closed independently.

## Boundary conflict to resolve

The current hq proposal base defines hq as a meaning-free compiler and human
acceptance surface that does not execute target effects. This snapshot includes
an LSP runtime, queue persistence, a runner, and a Windows host effect. That is
an intentional architecture comparison, not a silent expansion of official hq.

Before any adoption, a separate decision must determine whether these pieces:

1. become hq-owned runtime/worker packages;
2. remain downstream worker and target-adapter responsibilities;
3. split between hq compiler contracts and another effect owner.

The decision must also close the current lifecycle gap: `hq.submit` appends a
queue row, but the PoC has only `hq run --profile <name> --once`; it does not run
a continuous worker automatically.

## Verification

From this directory:

```text
go test -count=1 ./...
go vet ./...
go build ./cmd/hq
```

The test profile under `testdata/root` is repository-safe fixture data. Local
queues, receipts, binaries, and manual Vim proof files remain ignored runtime
artifacts.
