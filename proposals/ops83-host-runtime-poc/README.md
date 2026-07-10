# hq

Go implementation of the local hq LSP and host-command runtime.

```text
Vim buffer
  -> vim-lsp
    -> hq lsp --profile local
      -> profile catalog JSONL
      -> completion / diagnostics / hq.submit
      -> hq.hostCommandQueued.v1 JSONL
        -> hq run --profile local --once
          -> explorer.exe <path>
          -> hq.hostCommandReceipt.v1 JSONL
```

## Boundary

- The Vim buffer is the draft input. LSP `didOpen` and `didChange` carry it to hq.
- Completion is read from `profiles/<profile>/catalog.jsonl`.
- Completion and diagnostics are side-effect free.
- `hq.submit` validates the current document version before appending a queue row.
- The runner accepts the semantic operation `host.open`; it does not accept an arbitrary executable.
- On Windows, the adapter directly starts `explorer.exe` without a shell.
- Queue rows and receipts are local evidence, not accepted authority.

## Required local root

`HQ_LOCAL_ROOT` must contain all three JSONL files before hq starts:

```text
$HQ_LOCAL_ROOT/
  profiles/local/catalog.jsonl
  queues/hq.host-command.queue.jsonl
  receipts/hq.host-command.receipt.jsonl
```

No fallback path is created automatically.

## Commands

```text
hq doctor --profile local
hq lsp --profile local
hq run --profile local --once
```
