# Cursor context and JSONL world

Goal: add the first hq core boundary.

Scope:
- CursorContext
- JsonlWorld
- schema keys
- existing keys
- required keys

Done:
- context is derived from the current buffer
- JSONL data drives suggestions later
- route and prefix are not the core model

Proof:
- `python3 -m unittest discover -s tests` is the local close check.