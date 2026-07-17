# Provenance-safe accepted-input recall

Issue: #117

`accepted_input` is optional non-authoritative evidence beside one canonical
`accepted.instruction`. The instruction remains the only worker execution
meaning. Completion and selection remain read-only; explicit `hq.submit` remains
the sole accepted-log append.

## Field policy

Each `hq.command.v1` field may declare `history_policy` as `deny`, `recall`, or
`search`. Missing policy is `deny`. A field may declare `sensitive: true`; a
sensitive field permits only deny and may not carry value-bearing defaults,
examples, materialized values, or preset values.

- `deny`: do not retain the supplied name or value;
- `recall`: retain a safe typed value for exact-field recall only;
- `search`: retain a safe typed value for exact-field and free object queries.

## Accepted evidence

A successful strict selected-world `@command` submit derives accepted evidence
from the same parsed object used for lowering. It never reverses the canonical
instruction. Evidence carries exact world and command references, safe typed
fields in declaration order, supplied-field count, completeness, render
contract, finalized instruction digest, and its own canonical digest.

Identity-free and canonical-JSON compatibility submissions omit this evidence.
The worker strict envelope accepts the optional member and discards it before
constructing worker meaning.

## Projection

The production LSP reads the configured accepted file once at startup through a
read-only adapter. It retains only the intrinsically newest 1,000 rows and builds
one bounded in-memory projection. Completion never reads the file.

Eligible evidence is revalidated against the current selected world and exact
command contract. Legacy or row-local invalid evidence is excluded. Structural
or authority-integrity failure discards the whole history projection and leaves
the existing world-only index unchanged.

Bounds:

- 100 complete object groups;
- 50 history values per command field;
- 20 complete recent presets from the canonical empty draft;
- 50 completion items;
- five retained sources per group;
- 32 KiB accepted-input materialization;
- 8 KiB documentation preview;
- 512 Unicode code points per searchable accepted value.

Candidate identity excludes accepted ID, time, frequency, query, document
version, and row order. Equal semantic evidence ranks world declarations before
accepted history, then history by recency, frequency, and candidate ID.

No history database, profile path, provider lookup, editor redaction, worker
retry/replay meaning, or second durable store is introduced.
