#!/usr/bin/env python3
from __future__ import annotations

import re
import subprocess
import sys
import time
from pathlib import Path

ANSI_RE = re.compile(r"\x1b\[[0-?]*[ -/]*[@-~]")
SCHEMA = """\
{"key":"tabProofA","type":"string","required":true,"description":"tab proof key A"}
{"key":"tabProofB","type":"string","required":true,"description":"tab proof key B"}
{"key":"tabProofC","type":"string","required":true,"description":"tab proof key C"}
"""


def clean(s: str) -> str:
    return ANSI_RE.sub("", s).replace("\r", "\n")


def save(path: Path, status: str, sent: str, expected: list[str], transcript: str, error: str = "") -> None:
    path.write_text("\n".join([
        f"INTERACTIVE_TAB_COMPLETION_PROOF: {status}",
        "OS: windows-latest",
        "WINDOWS_TERMINAL_SESSION: wexpect/winpty",
        "PROOF_SCHEMA: temporary tabProofA/tabProofB/tabProofC keys not present in seeded history",
        "SENT_KEYS: {\" + <TAB>",
        f"SENT_BYTES_HEX: {sent.encode('utf-8').hex()}",
        "REQUIRED_VISIBLE_CANDIDATES: " + ", ".join(expected),
        "--- ERROR ---",
        error,
        "--- PROOF_SCHEMA_JSONL ---",
        SCHEMA.strip(),
        "--- CLEAN_TERMINAL_TRANSCRIPT ---",
        clean(transcript),
        "--- END_CLEAN_TERMINAL_TRANSCRIPT ---",
        "",
    ]), encoding="utf-8")
    print(f"windows tab proof {status}: {path}")


def main() -> int:
    if len(sys.argv) != 3:
        print("usage: windows-tab-proof-v2.py <exe> <proof.txt>", file=sys.stderr)
        return 2

    exe = str(Path(sys.argv[1]).resolve())
    proof_path = Path(sys.argv[2])
    proof_path.parent.mkdir(parents=True, exist_ok=True)
    schema_path = proof_path.with_suffix(".schema.jsonl")
    schema_path.write_text(SCHEMA, encoding="utf-8")

    sent = '{"\t'
    expected = ['"tabProofA"', '"tabProofB"', '"tabProofC"']
    transcript = ""
    child = None
    try:
        import wexpect  # type: ignore
        command = subprocess.list2cmdline([exe, "--schema", str(schema_path), "--no-banner"])
        child = wexpect.spawn(command, timeout=10)
        child.expect("hq>")
        transcript += str(getattr(child, "before", "")) + str(getattr(child, "after", ""))
        child.send(sent)
        deadline = time.monotonic() + 12
        while time.monotonic() < deadline:
            try:
                child.expect(expected, timeout=1)
            except Exception:
                pass
            transcript += str(getattr(child, "before", "")) + str(getattr(child, "after", ""))
            if all(x in clean(transcript) for x in expected):
                save(proof_path, "passed", sent, expected, transcript)
                return 0
        missing = [x for x in expected if x not in clean(transcript)]
        save(proof_path, "failed", sent, expected, transcript, "missing candidates: " + ", ".join(missing))
        return 1
    except Exception as exc:
        save(proof_path, "failed", sent, expected, transcript, repr(exc))
        return 1
    finally:
        if child is not None:
            try:
                child.close()
            except Exception:
                pass


if __name__ == "__main__":
    raise SystemExit(main())
