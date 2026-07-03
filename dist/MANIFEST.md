# dist artifacts

`dist/` is not source authority.

These files are generated proof artifacts from earlier implementation proof work and must not be treated as canonical source:

- `hq-reflective-linux-amd64`
- `hq-reflective-windows-amd64.exe`

The canonical runtime source is the Go command under `cmd/hq` and the Go core under `internal/hq`.

Policy:

1. New generated binaries must not be committed.
2. Reviewable binaries should be produced by GitHub Actions or release artifacts.
3. Existing proof binaries are retained only as legacy proof evidence until PR6 moves proof evidence under `docs/proofs/` or replaces them with CI/release artifacts.
4. Any future committed generated artifact requires an explicit manifest entry explaining why it is source-tree material and how to regenerate it.

This manifest satisfies #13 criterion 9 only as a transitional source-authority boundary. It does not claim final proof-doc cleanup is complete.
