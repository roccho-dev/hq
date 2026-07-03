# Interactive Tab proof

This proof protects the claim that hq supports literal Tab interaction in terminal sessions, not only non-interactive completion calls.

## Required markers

Both Linux and Windows proof transcripts must show:

```text
SENT_KEYS: {" + <TAB>
SENT_BYTES_HEX: 7b2209
"tabProofA"
"tabProofB"
"tabProofC"
```

Linux additionally requires:

```text
PTY_SESSION: real pseudo terminal
```

Windows additionally requires:

```text
WINDOWS_TERMINAL_SESSION: wexpect/winpty
```

## Current CI workflow

Workflow:

```text
Interactive tab proof
```

Jobs:

| Job | Required result |
|---|---|
| `linux-pty-tab-proof` | success |
| `windows-tab-proof` | success |

## Artifact naming boundary

The desired product artifact names are:

| Desired artifact | Meaning |
|---|---|
| `hq-interactive-tab-proof-linux` | Linux literal Tab proof |
| `hq-interactive-tab-proof-windows` | Windows literal Tab proof |

If a legacy workflow still emits proof-era names, reviewers must treat those names as compatibility evidence only. The product claim is the marker contract above, not the legacy artifact prefix.

## Non-goal

This proof does not prove all line-editor UX. It only proves literal Tab operation and visible proof-only candidates on both OS surfaces.
