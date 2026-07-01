# Semantic suggestion

Goal: make completion candidates meaningful and compile-ready.

Scope:
- label
- detail
- edit
- meaning
- compileDraft
- key suggestion rules

Done:
- a suggestion is not only display text
- every suggestion can explain its edit and future instruction
- key candidates avoid context noise

Proof:
- `python3 -m unittest discover -s tests` is the local close check.
