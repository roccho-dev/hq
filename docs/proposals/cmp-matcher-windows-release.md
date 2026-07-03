# Proposal: cmp-like matcher REPL and Rust Windows release path

Refs: #9

## Status

Proposal support artifact plus minimal Rust implementation. This does not claim that the full interactive REPL is complete.

## Purpose

The current product boundary says hq should read a JSONL world, read cursor context, suggest only context-valid candidates, finalize only the human-chosen candidate, and append durable instructions as JSONL.

This proposal narrows the next implementation route:

1. Keep hq core as a JSONL-aware candidate / queue-draft compiler.
2. Make the default REPL UI editor-like and low-noise.
3. Use a matcher-only fuzzy scorer for ranking suggestions.
4. Do not use fzf as the default completion UI.
5. Keep fzf-style deep picker behavior optional.
6. Build a Windows Rust artifact in CI.
7. Publish the Windows Rust artifact to GitHub Releases on tag pushes.

## UX decision

Default UI should be:

```text
inline hint / ghost suggestion
small cmp-like popup
mini queue draft pane
accept -> queue.create JSONL
```

Not default:

```text
fullscreen picker
fzf as completion replacement
vim/nvim as core dependency
```

## Why matcher-only

The recent local proof showed that the useful part for the default REPL is not a full fuzzy picker. It is only fuzzy ranking over already-valid hq suggestions.

Therefore the hq UI boundary should be:

```text
JsonlWorld + CursorContext
  -> Suggestion[] with compileDraft
  -> matcher ranks Suggestion[]
  -> cmp-like surface displays top suggestions
  -> accept creates queue instruction JSONL
```

This keeps the UI small and avoids making fzf, vim, or a full TUI the product model.

## Queue-draft requirement

The default REPL must not stop at single candidate insertion. It must also support queue-draft construction.

Required states:

```text
candidate.item
queue.draft.item
queue.draft.patch
queue.create
queue.created
dispatch.request
```

Minimum accepted behavior:

1. A candidate can be accepted into the input buffer.
2. A candidate can also be added into a queue draft.
3. The queue draft can be shown in a mini pane.
4. Accepting the draft emits a durable `queue.create` JSONL instruction.
5. Unaccepted candidates do not mutate the durable queue.

## Minimal Rust implementation included here

The Rust binary implements a small, real path for:

```text
hq suggest --buffer <json-fragment> [--query <text>]
hq accept  --buffer <json-fragment> [--query <text>] [--index <n>]
hq demo-autocomplete
```

It includes:

1. cursor context detection for key/value locations,
2. schema-like key and enum-value suggestions,
3. matcher-ranked fuzzy suggestions,
4. compileDraft JSON in each candidate,
5. queue.create JSONL output on accept,
6. Rust tests for fuzzy match, key suggestions, value suggestions, and queue.create output.

## What this PR intentionally does not implement

This proposal branch should not pretend to finish the full interactive REPL. It does not yet implement:

1. an interactive line editor,
2. inline ghost rendering,
3. mini queue pane rendering,
4. persistent queue-draft editing,
5. ctx dispatch execution.

Those should follow in issue-linked PRs.

## Release path

For now, the Windows release artifact is a Rust-built binary zip:

```text
hq-windows-rust.zip
  hq.exe
  README.txt
```

This intentionally does not include the Python package or a Python wrapper.

The included binary is now more than a release-path placeholder: it contains the minimal Rust suggestion/accept implementation above. It still does not claim that the full interactive REPL is complete.

## Merge readiness

This proposal becomes merge-ready when:

1. Linux CI remains green.
2. Windows Rust artifact workflow builds on pull request.
3. Tag trigger can create or update a GitHub Release asset.
4. PR body clearly states that full interactive REPL completion is still future work.
