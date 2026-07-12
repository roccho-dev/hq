# Data-driven command language

## Boundary

The editor buffer may contain human-facing, multi-line command objects instead
of requiring users to author canonical instruction objects directly.

```text
@host.open
path="C:\Users\resta\Codex"

@herdr.read
agent=reviewer
source=screen
lines=100
```

```text
command text
  -> generic hq parse / completion / validation
  -> hq.command.v1 definitions from the profile world
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

## Notebook behavior

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
- Submit assigns the canonical instruction identity and timestamp.
- Existing lines remain editor history; the managed worker observes only the
  canonical accepted queue, never the live Vim buffer.

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
