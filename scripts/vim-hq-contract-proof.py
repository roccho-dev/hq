#!/usr/bin/env python3
"""Prove the Vim-to-hq CLI boundary from checked-in JSONL fixtures."""

from __future__ import annotations

import argparse
import json
import subprocess
import tempfile
from pathlib import Path
from typing import Any


FIXTURE_PATH = Path("spec/fixtures/vim-hq.contract.jsonl")


def load_rows(path: Path) -> list[dict[str, Any]]:
    rows: list[dict[str, Any]] = []
    for line_number, raw in enumerate(path.read_text(encoding="utf-8").splitlines(), start=1):
        if not raw.strip():
            continue
        row = json.loads(raw)
        if not isinstance(row, dict):
            raise AssertionError(f"{path}:{line_number}: fixture row must be an object")
        rows.append(row)
    if not rows:
        raise AssertionError(f"{path}: fixture must contain at least one row")
    return rows


def run_json(binary: Path, args: list[str], cwd: Path) -> Any:
    proc = subprocess.run(
        [str(binary), *args],
        cwd=cwd,
        check=False,
        capture_output=True,
        text=True,
        encoding="utf-8",
    )
    if proc.returncode != 0:
        raise AssertionError(
            f"hq exited {proc.returncode}: args={args!r}\nstdout={proc.stdout}\nstderr={proc.stderr}"
        )
    try:
        return json.loads(proc.stdout)
    except json.JSONDecodeError as exc:
        raise AssertionError(f"hq did not return JSON: {proc.stdout!r}") from exc


def queue_rows(path: Path) -> list[dict[str, Any]]:
    if not path.exists():
        return []
    return [json.loads(line) for line in path.read_text(encoding="utf-8").splitlines() if line.strip()]


def prove(binary: Path, fixture_path: Path) -> dict[str, Any]:
    binary = binary.resolve()
    fixture_path = fixture_path.resolve()
    rows = load_rows(fixture_path)
    cases = {row.get("case"): row for row in rows}
    required_cases = {"complete", "draft", "accept"}
    if set(cases) != required_cases:
        raise AssertionError(f"fixture cases must be exactly {sorted(required_cases)}; got {sorted(cases)}")

    with tempfile.TemporaryDirectory(prefix="hq-vim-contract-") as tmp:
        work = Path(tmp)
        queue = work / "instruction.jsonl"

        complete_case = cases["complete"]
        complete_args = [
            "--complete",
            complete_case["buffer"],
            "--cursor",
            str(complete_case["cursor"]),
            "--queue",
            str(queue),
        ]
        complete = run_json(binary, complete_args, work)
        if not isinstance(complete, list):
            raise AssertionError(f"complete output must be a list: {complete!r}")
        match = next((row for row in complete if row.get("label") == complete_case["expectedLabel"]), None)
        if match is None:
            raise AssertionError(f"missing expected completion label: {complete_case['expectedLabel']}")
        if match.get("compileDraft", {}).get("queue") != complete_case["expectedQueue"]:
            raise AssertionError(f"completion compileDraft queue mismatch: {match!r}")
        if queue_rows(queue):
            raise AssertionError("completion must not append queue rows")

        draft_case = cases["draft"]
        draft = run_json(
            binary,
            ["--draft", draft_case["buffer"], "--queue", str(queue)],
            work,
        )
        if draft.get("kind") != draft_case["expectedKind"]:
            raise AssertionError(f"draft kind mismatch: {draft!r}")
        if draft.get("queue") != draft_case["expectedQueue"]:
            raise AssertionError(f"draft queue mismatch: {draft!r}")
        if queue_rows(queue):
            raise AssertionError("draft must not append queue rows")

        accept_case = cases["accept"]
        accept_without_queue = run_json(binary, ["--accept", accept_case["buffer"]], work)
        if accept_without_queue.get("kind") != accept_case["expectedKind"]:
            raise AssertionError(f"accept-without-queue kind mismatch: {accept_without_queue!r}")
        if queue_rows(queue):
            raise AssertionError("accept without --queue must not create the explicit queue")
        if (work / accept_case["expectedQueue"]).exists():
            raise AssertionError("accept without --queue must not create an implicit queue file")

        accepted = run_json(
            binary,
            ["--accept", accept_case["buffer"], "--queue", str(queue)],
            work,
        )
        written = queue_rows(queue)
        if len(written) != accept_case["expectedRows"]:
            raise AssertionError(f"accept row count mismatch: {written!r}")
        if written[0] != accepted:
            raise AssertionError("the one appended row must equal the accepted output")
        if accepted.get("kind") != accept_case["expectedKind"]:
            raise AssertionError(f"accepted kind mismatch: {accepted!r}")
        if accepted.get("queue") != accept_case["expectedQueue"]:
            raise AssertionError(f"accepted queue mismatch: {accepted!r}")

        return {
            "kind": "hq.vimContractProof.v1",
            "status": "passed",
            "fixture": str(fixture_path),
            "cases": ["complete", "draft", "accept"],
            "completionLabel": complete_case["expectedLabel"],
            "queue": accept_case["expectedQueue"],
            "acceptedRows": len(written),
            "completionWrites": 0,
            "draftWrites": 0,
            "implicitAcceptWrites": 0,
            "boundary": "vim -> hq complete/draft/accept -> optional one-row append; worker/adapters remain downstream",
        }


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--binary", required=True, type=Path)
    parser.add_argument("--fixture", type=Path, default=FIXTURE_PATH)
    parser.add_argument("--artifact", type=Path)
    args = parser.parse_args()

    proof = prove(args.binary, args.fixture)
    rendered = json.dumps(proof, ensure_ascii=False, sort_keys=True, indent=2) + "\n"
    if args.artifact:
        args.artifact.parent.mkdir(parents=True, exist_ok=True)
        args.artifact.write_text(rendered, encoding="utf-8")
    print(rendered, end="")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
