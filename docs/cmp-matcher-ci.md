# CMP matcher CI

Refs #9.

This PR keeps the local completion UI evidence reproducible in the repository.

The exact local PoC source is stored as base64 parts under:

```text
proofs/nucleo-matcher-cmp/src/hq_fuzzy_cmp_demo.c.b64.part01..07
```

CI reconstructs that source, builds it with gcc, runs all ten proof cases, renders PNG screenshots, and uploads the artifact.

Expected cases:

1. `01_key_aid`
2. `02_value_aex`
3. `03_action_arcx`
4. `04_host_nxsh`
5. `05_tool_rg`
6. `06_queue_draft`
7. `07_ambiguous_fi`
8. `08_none`
9. `09_multi_token`
10. `10_queue_edit`

The interactive REPL remains future work.
