#!/usr/bin/env python3
"""Interactive PTY proof for hq-reflective Tab completion.

This starts hq-reflective in a real pseudo terminal, sends a partial JSON object
plus a literal Tab byte, and asserts that visible completion output contains
schema key candidates. It uses only the Python standard library.
"""

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


def read_until(fd: int, deadline: float, predicate) -> str:
    chunks: list[bytes] = []
    while time.monotonic() < deadline:
        ready, _, _ = select.select([fd], [], [], 0.05)
        if fd not in ready:
            continue
        try:
            data = os.read(fd, 4096)
        except OSError:
            break
        if not data:
            break
        chunks.append(data)
        text = b"".join(chunks).decode("utf-8", errors="replace")
        if predicate(text):
            return text
    return b"".join(chunks).decode("utf-8", errors="replace")


def strip_ansi(text: str) -> str:
    return ANSI_RE.sub("", text).replace("\r", "\n")


def terminate(proc: subprocess.Popen[bytes], fd: int) -> None:
    try:
        os.write(fd, b"\x03")
    except OSError:
        pass
    try:
        proc.wait(timeout=2)
        return
    except subprocess.TimeoutExpired:
        pass
    proc.terminate()
    try:
        proc.wait(timeout=2)
        return
    except subprocess.TimeoutExpired:
        pass
    proc.kill()
    proc.wait(timeout=2)


def main() -> int:
    if len(sys.argv) != 3:
        print("usage: pty-tab-proof.py <hq-reflective-exe> <transcript-path>", file=sys.stderr)
        return 2

    exe = sys.argv[1]
    transcript_path = Path(sys.argv[2])
    transcript_path.parent.mkdir(parents=True, exist_ok=True)

    input_bytes = b'{"\t'
    required_candidates = ['"op"', '"target"', '"payload"']

    master, slave = pty.openpty()
    env = os.environ.copy()
    env.setdefault("TERM", "xterm-256color")

    proc = subprocess.Popen(
        [exe, "--no-banner"],
        stdin=slave,
        stdout=slave,
        stderr=slave,
        env=env,
        close_fds=True,
        start_new_session=True,
    )
    os.close(slave)

    transcript = ""
    try:
        transcript += read_until(master, time.monotonic() + 5, lambda text: "hq>" in text)
        os.write(master, input_bytes)
        transcript += read_until(
            master,
            time.monotonic() + 8,
            lambda text: all(candidate in strip_ansi(text) for candidate in required_candidates),
        )
    finally:
        terminate(proc, master)
        try:
            os.close(master)
        except OSError:
            pass

    clean = strip_ansi(transcript)
    missing = [candidate for candidate in required_candidates if candidate not in clean]

    proof = "\n".join(
        [
            "INTERACTIVE_TAB_COMPLETION_PROOF: passed" if not missing else "INTERACTIVE_TAB_COMPLETION_PROOF: failed",
            "PTY_SESSION: real pseudo terminal",
            "SENT_KEYS: {\" + <TAB>",
            f"SENT_BYTES_HEX: {input_bytes.hex()}",
            "ASSERTION: literal Tab byte 0x09 was written to the PTY after the partial buffer {\"",
            "REQUIRED_VISIBLE_CANDIDATES: " + ", ".join(required_candidates),
            "--- CLEAN_TERMINAL_TRANSCRIPT ---",
            clean,
            "--- END_CLEAN_TERMINAL_TRANSCRIPT ---",
            "",
        ]
    )
    transcript_path.write_text(proof, encoding="utf-8")

    if missing:
        print("interactive Tab completion proof failed", file=sys.stderr)
        print(f"missing candidates: {missing}", file=sys.stderr)
        print(proof, file=sys.stderr)
        return 1

    print(proof)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
