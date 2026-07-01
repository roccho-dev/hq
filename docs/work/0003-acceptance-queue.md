# Acceptance and instruction queue

Goal: finalize only human-accepted suggestions.

Scope:
- Acceptance
- InstructionQueue
- JSONL append
- accept-before-queue behavior

Done:
- unaccepted suggestions never enter queue
- accepted suggestions become final instructions
- final instructions append to JSONL queue
