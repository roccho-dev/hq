# hq

hq is a small JSONL-aware autocomplete compiler.

The first product boundary is:

1. read the JSONL world,
2. read the current cursor context,
3. suggest only context-valid candidates,
4. finalize only the human-chosen candidate,
5. append durable instructions as JSONL.

This keeps the core reusable for terminal, CLI, and REPL surfaces without making route or prefix the product model.

This proposal branch also includes a minimal Rust `hq` binary proof for matcher-ranked `suggest` and `accept` output. It does not claim the full interactive REPL is complete.
