#!/usr/bin/env python3
from __future__ import annotations

import os
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


def clean(text: str) -> str:
    return ANSI_RE.sub("", text).replace("\r", "\n")


def main() -> int:
    if len(sys.argv) != 3:
        print("usage: windows-tab-proof.py <exe> <proof.txt>", file=sys.stderr)
        return 2

    try:
        import wexpect  # type: ignore
    except Exception as exc:  # pragma: no cover - CI environment proof dependency
        print(f"failed to import wexpect: {exc}", file=sys.stderr)
        return 2

    exe = str(Path(sys.argv[1]).resolve())
    proof_path = Path(sys.argv[2])
    proof_path.parent.mkdir(parents=True, exist_ok=True)
    schema_path = proof_path.with_suffix(".schema.jsonl")
    schema_path.write_text(SCHEMA, encoding="utf-8")

    sent = '{"\t'
    expected = ['"tabProofA"', '"tabProofB"', '"tabProofC"']
    command = subprocess.list2cmdline([exe, "--schema", str(schema_path), "--no-banner"])

    transcript = ""
    status = "failed"
    missing = expected[:]
    child = None
    try:
        try:
            child = wexpect.spawn(command, timeout=10, encoding="utf-8")
        except TypeError:
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
            text = clean(transcript)
            missing = [x for x in expected if x not in text]
            if not missing:
                status = "passed"
                break
        try:
            child.sendcontrol("c")
        except Exception:
            pass
    finally:
        if child is not None:
            try:
                child.close(force=True)
            except Exception:
                pass

    text = clean(transcript)
    proof = "\n".join([
        f"INTERACTIVE_TAB_COMPLETION_PROOF: {status}",
        "OS: windows-latest",
        "WINDOWS_TERMINAL_SESSION: wexpect/winpty",
        "PROOF_SCHEMA: temporary tabProofA/tabProofB/tabProofC keys not present in seeded history",
        "SENT_KEYS: {\" + <TAB>",
        f"SENT_BYTES_HEX: {sent.encode('utf-8').hex()}",
        "REQUIRED_VISIBLE_CANDIDATES: " + ", ".join(expected),
        "--- PROOF_SCHEMA_JSONL ---",
        SCHEMA.strip(),
        "--- CLEAN_TERMINAL_TRANSCRIPT ---",
        text,
        "--- END_CLEAN_TERMINAL_TRANSCRIPT ---",
        "",
    ])
    proof_path.write_text(proof, encoding="utf-8")
    print(proof)
    if missing:
        print(f"missing candidates: {missing}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
