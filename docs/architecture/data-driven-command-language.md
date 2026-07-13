# Data-driven command language

## Boundary

The editor buffer is a disposable draft surface for human-facing, multi-line
command objects. It is not a durable history or execution authority and does
not require users to author canonical instruction objects directly.

```text
@host.open
path="C:\Users\resta\Codex"

@herdr.read
agent=reviewer
source=screen
lines=100
```

```text
command draft
  -> generic hq parse / completion / validation
  -> hq.command.v1 definitions from the profile world
  -> explicit submit
  -> accepted.instruction
  -> canonical instruction.v1
```

The LSP server transports document state, completion items, diagnostics, code
actions, and explicit submit results. It owns no command vocabulary or target
meaning. The compiler owns only the generic `name=value` parser, declared type
checks, edit ranges, and declarative field binding.

## World records

Each `hq.command.v1` row declares:

- the human-facing command name and description;
- the base canonical instruction object;
- accepted fields and their primitive types;
- required fields, enum values, examples, and descriptions;
- the object path into which each accepted value is lowered.

Adding a valid command row changes completion, diagnostics, and lowering
without changing the hq binary or the LSP adapter. Unsupported record kinds,
duplicate commands or fields, unsupported primitive types, and missing binding
paths fail while loading the world.

## Disposable draft behavior

- An object starts at `@command` and ends immediately before the next
  `@command` or at end of file.
- Blank lines are allowed inside an object and do not delimit it.
- Each non-empty field line is exactly `name=value`.
- Completion is derived from the command object and field at the cursor.
- A confirmed command or field-key candidate replaces the complete current
  line; a value candidate replaces only the right side of `=`.
- Diagnostics validate each object independently and reject non-empty field
  lines that occur before any `@command` header.
- Explicit submit lowers only the object containing the LSP code-action line.
- Submit assigns a fresh canonical instruction identity and acceptance time.
- Selection and editing change only the draft and append no accepted row.
- The buffer may contain multiple temporary objects, but none is durable
  history or worker input until explicitly submitted.
- The buffer may be wiped without deleting accepted history.
- Durable recall history, when enabled by #114/#117, is derived only from
  provenance-complete accepted-input evidence, never from buffer lines, Vim
  history, registers, swap, undo files, or terminal history.
- The managed worker observes only the canonical accepted queue, never the live
  Vim buffer or accepted-input presentation evidence.

The proof does not define or install a custom Tab mapping. Completion items use
standard LSP `textEdit`; editor-specific acceptance UX remains an editor-client
decision.

Comments, nested editor syntax, inline command arguments, implicit indentation
meaning, and a second delimiter token are intentionally absent.

## Meaning and execution

Command definitions carry domain meaning as profile world data. The hq
compiler and LSP packages do not name Explorer, Herdr executables, provider
paths, shell commands, or installation layouts. The canonical instruction
contains semantic target and payload only. A worker validates that instruction
and resolves any concrete provider through the active envs capability binding.

[`examples/hq.commands.jsonl`](../../examples/hq.commands.jsonl) proves two
different command patterns with the same binary: `host.open` and `herdr.read`.
