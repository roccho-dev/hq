# hq

Small JSONL-aware autocomplete compiler for terminal-first input.

This repository starts from a narrow product core:

- understand JSONL world and cursor context
- produce compile-ready suggestions
- finalize only accepted suggestions
- append accepted instructions to a JSONL queue
- keep CLI/REPL previews reproducible
