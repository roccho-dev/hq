#!/usr/bin/env python3
from __future__ import annotations

import os
import pty
import re
import select
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


def read_some(fd: int, seconds: float, stop) -> str:
    end = time.monotonic() + seconds
    out: list[bytes] = []
    while time.monotonic() < end:
        r, _, _ = select.select([fd], [], [], 0.05)
        if not r:
            continue
        try:
            b = os.read(fd, 8192)
        except OSError:
            break
        if not b:
            break
        out.append(b)
        text = b"".join(out).decode("utf-8", "replace")
        if stop(text):
            break
    return b"".join(out).decode("utf-8", "replace")


def main() -> int:
    if len(sys.argv) != 3:
        print("usage: pty-tab-proof-v2.py <exe> <proof.txt>", file=sys.stderr)
        return 2

    exe = sys.argv[1]
    proof_path = Path(sys.argv[2])
    proof_path.parent.mkdir(parents=True, exist_ok=True)
    schema_path = proof_path.with_suffix(".schema.jsonl")
    schema_path.write_text(SCHEMA, encoding="utf-8")

    sent = b'{"\t'
    expected = ['"tabProofA"', '"tabProofB"', '"tabProofC"']

    master, slave = pty.openpty()
    env = os.environ.copy()
    env.setdefault("TERM", "xterm-256color")
    proc = subprocess.Popen(
        [exe, "--schema", str(schema_path), "--no-banner"],
        stdin=slave,
        stdout=slave,
        stderr=slave,
        env=env,
        close_fds=True,
    )
    os.close(slave)

    transcript = ""
    try:
        transcript += read_some(master, 5, lambda t: "hq>" in clean(t))
        os.write(master, sent)
        transcript += read_some(master, 8, lambda t: all(x in clean(t) for x in expected))
        try:
            os.write(master, b"\x03")
        except OSError:
            pass
        try:
            proc.wait(timeout=2)
        except subprocess.TimeoutExpired:
            proc.terminate()
            proc.wait(timeout=2)
    finally:
        try:
            os.close(master)
        except OSError:
            pass

    text = clean(transcript)
    missing = [x for x in expected if x not in text]
    status = "passed" if not missing else "failed"
    proof = "\n".join([
        f"INTERACTIVE_TAB_COMPLETION_PROOF: {status}",
        "PTY_SESSION: real pseudo terminal",
        "PROOF_SCHEMA: temporary tabProofA/tabProofB/tabProofC keys not present in seeded history",
        "SENT_KEYS: {\" + <TAB>",
        f"SENT_BYTES_HEX: {sent.hex()}",
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
