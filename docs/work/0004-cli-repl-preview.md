# CLI REPL preview

Goal: expose hq to terminal users.

Scope:
- hq complete
- choose flow
- preview HTML

Done:
- CLI returns stable output
- candidate selection returns one final JSONL row
- preview is generated from the same completion payload as CLI output

Proof:
- `python3 -m unittest discover -s tests` is the local close check.
