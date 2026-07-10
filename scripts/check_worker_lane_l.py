#!/usr/bin/env python3
"""Regenerate lane-L contract, dry-run, provider, E2E, and readback proof."""

from __future__ import annotations

import hashlib
import json
import os
import platform
import shutil
import subprocess
import sys
import tempfile
import traceback
from collections import defaultdict
from pathlib import Path
from typing import Any, Iterable

ROOT = Path(__file__).resolve().parents[1]
ARTIFACT = ROOT / "artifacts" / "worker-lane-l"
IS_WINDOWS = os.name == "nt"
EXE = ".exe" if IS_WINDOWS else ""


def write_text(path: Path, value: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(value, encoding="utf-8")


def write_json(path: Path, value: Any) -> None:
    write_text(path, json.dumps(value, ensure_ascii=False, indent=2, sort_keys=True) + "\n")


def write_jsonl(path: Path, rows: Iterable[dict[str, Any]]) -> None:
    write_text(path, "".join(json.dumps(row, ensure_ascii=False, separators=(",", ":")) + "\n" for row in rows))


def read_jsonl(path: Path) -> list[dict[str, Any]]:
    if not path.exists():
        return []
    return [json.loads(line) for line in path.read_text(encoding="utf-8").splitlines() if line.strip()]


def run(
    command: list[str],
    *,
    expected: tuple[int, ...] = (0,),
    env: dict[str, str] | None = None,
    stdout_path: Path | None = None,
    stderr_path: Path | None = None,
) -> subprocess.CompletedProcess[str]:
    completed = subprocess.run(
        command,
        cwd=ROOT,
        env=env,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        check=False,
    )
    if stdout_path is not None:
        write_text(stdout_path, completed.stdout)
    if stderr_path is not None:
        write_text(stderr_path, completed.stderr)
    if completed.returncode not in expected:
        raise AssertionError(
            f"command failed ({completed.returncode}, expected {expected}): {command}\n"
            f"stdout:\n{completed.stdout}\nstderr:\n{completed.stderr}"
        )
    return completed


def sha256_file(path: Path) -> str:
    return "sha256:" + hashlib.sha256(path.read_bytes()).hexdigest()


def executable(path: Path) -> str:
    return str(path.resolve())


def build_binaries() -> dict[str, Path]:
    binary_dir = ARTIFACT / "bin"
    binary_dir.mkdir(parents=True, exist_ok=True)
    binaries = {
        "hq": binary_dir / f"hq{EXE}",
        "worker": binary_dir / f"hq-worker{EXE}",
        "provider": binary_dir / f"hq-worker-proof-provider{EXE}",
    }
    run(["go", "build", "-o", str(binaries["hq"]), "./cmd/hq"])
    run(["go", "build", "-o", str(binaries["worker"]), "./cmd/hq-worker"])
    run(["go", "build", "-o", str(binaries["provider"]), "./cmd/hq-worker-proof-provider"])
    for provider in ("sh", "herdr", "codex", "claude"):
        destination = binary_dir / f"proof-{provider}{EXE}"
        shutil.copy2(binaries["provider"], destination)
        if not IS_WINDOWS:
            destination.chmod(0o755)
        binaries[provider] = destination
    return binaries


def contract_proof() -> None:
    run(
        [sys.executable, "-m", "unittest", "-v", "tests/test_protocol_contract.py"],
        stdout_path=ARTIFACT / "contract-fixture-test.stdout.log",
        stderr_path=ARTIFACT / "contract-fixture-test.stderr.log",
    )
    fixture_paths = [
        ROOT / "spec/fixtures/instruction.examples.jsonl",
        ROOT / "spec/fixtures/instruction.invalid.jsonl",
        ROOT / "spec/fixtures/result.runs.jsonl",
        ROOT / "spec/fixtures/session.index.jsonl",
        ROOT / "spec/fixtures/status.transitions.jsonl",
        ROOT / "spec/fixtures/agent-adapters/instructions.jsonl",
    ]
    matrix = []
    for path in fixture_paths:
        rows = read_jsonl(path)
        matrix.append(
            {
                "path": str(path.relative_to(ROOT)).replace("\\", "/"),
                "rows": len(rows),
                "sha256": sha256_file(path),
                "valid_or_negative": "negative" if "invalid" in path.name else "valid",
            }
        )
    write_json(
        ARTIFACT / "contract-fixture-matrix.json",
        {
            "version": "worker.contract-fixture-matrix.v1",
            "valid_fixtures_pass": True,
            "invalid_fixtures_fail_closed": True,
            "fixtures": matrix,
        },
    )


def source_instructions(binaries: dict[str, Path], marker: Path) -> list[dict[str, Any]]:
    literal = f"; touch {marker} && echo $(uname)"
    rows = [
        {
            "id": "lane-l-sh-001",
            "version": "instruction.v1",
            "op": "run",
            "target": "sh",
            "payload": {"cwd": ".", "argv": [executable(binaries["sh"]), "echo", literal]},
            "created_at": "2026-07-10T07:20:00Z",
            "reason": "lane L direct argv and injection proof",
            "labels": ["lane-l", "fixture-proof"],
        },
        {
            "id": "lane-l-herdr-start-001",
            "version": "instruction.v1",
            "op": "run",
            "target": "herdr",
            "payload": {
                "action": "start",
                "cwd": ".",
                "name": "lane-l-proof",
                "no_focus": True,
                "split": "right",
                "command_argv": [executable(binaries["claude"]), "-p"],
                "prompt": "Return deterministic proof.",
            },
            "created_at": "2026-07-10T07:21:00Z",
        },
        {
            "id": "lane-l-herdr-read-001",
            "version": "instruction.v1",
            "op": "run",
            "target": "herdr",
            "payload": {
                "action": "read",
                "cwd": ".",
                "agent": "terminal-proof-1",
                "source": "recent-unwrapped",
                "lines": 100,
            },
            "created_at": "2026-07-10T07:22:00Z",
        },
        {
            "id": "lane-l-herdr-observe-001",
            "version": "instruction.v1",
            "op": "run",
            "target": "herdr",
            "payload": {
                "action": "observe",
                "cwd": ".",
                "agent": "terminal-proof-1",
                "wait_status": "idle",
                "timeout_ms": 1000,
                "lines": 100,
            },
            "created_at": "2026-07-10T07:23:00Z",
        },
        {
            "id": "lane-l-herdr-attach-001",
            "version": "instruction.v1",
            "op": "run",
            "target": "herdr",
            "payload": {
                "action": "attach",
                "cwd": ".",
                "agent": "terminal-proof-1",
                "takeover": False,
            },
            "created_at": "2026-07-10T07:24:00Z",
        },
        {
            "id": "lane-l-codex-exec-001",
            "version": "instruction.v1",
            "op": "run",
            "target": "codex",
            "payload": {
                "action": "exec",
                "prompt": "Return Codex lane L proof.",
                "cwd": ".",
                "output_path": ".hq/final/codex-exec.txt",
                "sandbox": "workspace-write",
                "skip_git_repo_check": True,
            },
            "created_at": "2026-07-10T07:25:00Z",
        },
        {
            "id": "lane-l-codex-resume-001",
            "version": "instruction.v1",
            "op": "run",
            "target": "codex",
            "payload": {
                "action": "resume",
                "prompt": "Continue Codex lane L proof.",
                "session_id": "thread-proof-1",
                "cwd": ".",
                "output_path": ".hq/final/codex-resume.txt",
                "sandbox": "workspace-write",
                "skip_git_repo_check": True,
            },
            "created_at": "2026-07-10T07:26:00Z",
            "reply_to": "lane-l-codex-exec-001",
        },
        {
            "id": "lane-l-claude-print-001",
            "version": "instruction.v1",
            "op": "run",
            "target": "claude",
            "payload": {
                "action": "print",
                "prompt": "Return Claude lane L proof.",
                "cwd": ".",
                "output_format": "json",
                "max_turns": 1,
            },
            "created_at": "2026-07-10T07:27:00Z",
        },
        {
            "id": "lane-l-claude-resume-001",
            "version": "instruction.v1",
            "op": "run",
            "target": "claude",
            "payload": {
                "action": "resume",
                "prompt": "Continue Claude lane L proof.",
                "session_id": "session-proof-1",
                "cwd": ".",
                "output_format": "stream-json",
                "max_turns": 1,
            },
            "created_at": "2026-07-10T07:28:00Z",
            "reply_to": "lane-l-claude-print-001",
        },
    ]
    return rows


def accept_instructions(hq: Path, rows: list[dict[str, Any]], queue: Path) -> None:
    outputs = []
    for row in rows:
        completed = run(
            [executable(hq), "--accept", json.dumps(row, ensure_ascii=False, separators=(",", ":")), "--queue", str(queue)]
        )
        outputs.append(json.loads(completed.stdout))
    write_jsonl(ARTIFACT / "accepted-command-output.jsonl", outputs)
    shutil.copy2(queue, ARTIFACT / "accepted-instructions.jsonl")


def make_approvals(plan_rows: list[dict[str, Any]], path: Path) -> None:
    approvals = [
        {
            "version": "worker.approval.v1",
            "instruction_id": row["instruction_id"],
            "approved": True,
            "approved_by": "lane-l-ci",
            "instruction_digest": row["instruction_digest"],
        }
        for row in plan_rows
    ]
    write_jsonl(path, approvals)
    shutil.copy2(path, ARTIFACT / "approvals.jsonl")


def project_sessions(ledger: list[dict[str, Any]]) -> list[dict[str, Any]]:
    sessions = []
    for row in ledger:
        session = {
            "version": "session.v1",
            "run_id": row["run_id"],
            "instruction_id": row["instruction_id"],
            "target": row["target"],
            "status": row["status"],
            "cwd": row["cwd"],
            "started_at": row["started_at"],
            "last_event_at": row["last_event_at"],
        }
        if row.get("native_session_id"):
            session["native_session_id"] = row["native_session_id"]
        if row.get("final_path"):
            session["final_path"] = row["final_path"]
        sessions.append(session)
    return sessions


def run_whole_path(binaries: dict[str, Path]) -> None:
    work = Path(tempfile.mkdtemp(prefix="hq-worker-lane-l-"))
    try:
        project = work / "project"
        project.mkdir(parents=True)
        marker = project / "injection-must-not-exist"
        source_rows = source_instructions(binaries, marker)
        write_jsonl(ARTIFACT / "source-instructions.jsonl", source_rows)

        accepted = work / "accepted.jsonl"
        accept_instructions(binaries["hq"], source_rows, accepted)

        provider_log = ARTIFACT / "provider-invocations.jsonl"
        events = work / "events.jsonl"
        plan_path = ARTIFACT / "dry-run-plan.jsonl"
        dry = run(
            [
                executable(binaries["worker"]),
                "--input", str(accepted),
                "--input-format", "accepted.instruction",
                "--events", str(events),
                "--workspace", str(project),
                "--dry-run",
            ],
            stdout_path=plan_path,
            stderr_path=ARTIFACT / "dry-run.stderr.log",
        )
        if dry.returncode != 0 or events.exists() or provider_log.exists() or marker.exists():
            raise AssertionError("dry-run caused a side effect")
        plan_rows = read_jsonl(plan_path)
        if len(plan_rows) != len(source_rows) or any(row["decision"] != "accepted" for row in plan_rows):
            raise AssertionError(f"unexpected accepted dry-run plan: {plan_rows}")

        invalid_input = work / "invalid-instructions.jsonl"
        invalid_rows = [
            {
                "id": "lane-l-invalid-target",
                "version": "instruction.v1",
                "op": "run",
                "target": "unknown",
                "payload": {},
                "created_at": "2026-07-10T07:29:00Z",
            },
            {
                "id": "lane-l-invalid-argv",
                "version": "instruction.v1",
                "op": "run",
                "target": "sh",
                "payload": {"argv": []},
                "created_at": "2026-07-10T07:30:00Z",
            },
        ]
        write_jsonl(invalid_input, invalid_rows)
        invalid = run(
            [
                executable(binaries["worker"]),
                "--input", str(invalid_input),
                "--input-format", "instruction.v1",
                "--events", str(events),
                "--workspace", str(project),
                "--dry-run",
            ],
            expected=(2,),
            stdout_path=ARTIFACT / "dry-run-invalid-plan.jsonl",
            stderr_path=ARTIFACT / "dry-run-invalid.stderr.log",
        )
        invalid_plans = [json.loads(line) for line in invalid.stdout.splitlines() if line.strip()]
        if len(invalid_plans) != 2 or any(row["decision"] != "blocked" or not row["validation"] for row in invalid_plans):
            raise AssertionError(f"invalid dry-run did not fail closed: {invalid_plans}")
        if events.exists() or provider_log.exists() or marker.exists():
            raise AssertionError("invalid dry-run caused a side effect")

        approvals = work / "approvals.jsonl"
        make_approvals(plan_rows, approvals)
        environment = os.environ.copy()
        environment.update(
            {
                "HQ_HERDR_PATH": executable(binaries["herdr"]),
                "HQ_CODEX_PATH": executable(binaries["codex"]),
                "HQ_CLAUDE_PATH": executable(binaries["claude"]),
                "HQ_PROOF_PROVIDER_LOG": str(provider_log),
            }
        )
        run(
            [
                executable(binaries["worker"]),
                "--input", str(accepted),
                "--input-format", "accepted.instruction",
                "--events", str(events),
                "--approvals", str(approvals),
                "--workspace", str(project),
            ],
            env=environment,
            stdout_path=ARTIFACT / "execution-output.jsonl",
            stderr_path=ARTIFACT / "execution.stderr.log",
        )
        if marker.exists():
            raise AssertionError("metacharacter argv was reparsed by a shell")

        event_rows = read_jsonl(events)
        result_rows = [row for row in event_rows if row.get("version") == "result.v1"]
        grouped: dict[str, list[dict[str, Any]]] = defaultdict(list)
        for row in result_rows:
            grouped[row["instruction_id"]].append(row)
        if set(grouped) != {row["id"] for row in source_rows}:
            raise AssertionError("not every accepted instruction has durable result evidence")
        for instruction_id, rows in grouped.items():
            rows.sort(key=lambda row: row["seq"])
            if rows[-1]["kind"] != "completed":
                raise AssertionError(f"{instruction_id} did not complete: {rows[-1]}")
            if "final" not in rows[-1]:
                raise AssertionError(f"{instruction_id} has no durable final result")

        invocations_before = read_jsonl(provider_log)
        if len(invocations_before) != 10:
            raise AssertionError(f"unexpected provider process count: {len(invocations_before)}")
        for invocation in invocations_before:
            if invocation.get("fixture_only") is not True or not invocation.get("argv"):
                raise AssertionError(f"uninspectable provider invocation: {invocation}")

        duplicate = run(
            [
                executable(binaries["worker"]),
                "--input", str(accepted),
                "--input-format", "accepted.instruction",
                "--events", str(events),
                "--approvals", str(approvals),
                "--workspace", str(project),
            ],
            expected=(2,),
            env=environment,
            stdout_path=ARTIFACT / "duplicate-read-output.jsonl",
            stderr_path=ARTIFACT / "duplicate-read.stderr.log",
        )
        duplicate_rows = [json.loads(line) for line in duplicate.stdout.splitlines() if line.strip()]
        if len(duplicate_rows) != len(source_rows) or any(
            row.get("version") != "validation.v1" or row.get("error", {}).get("code") != "duplicate_id"
            for row in duplicate_rows
        ):
            raise AssertionError(f"duplicate re-read was not rejected: {duplicate_rows}")
        if len(read_jsonl(provider_log)) != len(invocations_before):
            raise AssertionError("duplicate re-read executed a provider")

        shutil.copy2(events, ARTIFACT / "events.jsonl")
        ledger_path = ARTIFACT / "ledger.jsonl"
        run(
            [
                executable(binaries["worker"]), "list",
                "--input", str(accepted),
                "--input-format", "accepted.instruction",
                "--events", str(events),
                "--limit", "0",
                "--json",
            ],
            stdout_path=ledger_path,
            stderr_path=ARTIFACT / "ledger.stderr.log",
        )
        ledger = read_jsonl(ledger_path)
        if len(ledger) != len(source_rows) or any(row["status"] != "completed" for row in ledger):
            raise AssertionError(f"ledger readback is incomplete: {ledger}")
        write_jsonl(ARTIFACT / "session-projection.jsonl", project_sessions(ledger))

        show_rows = []
        by_target: dict[str, dict[str, Any]] = {}
        for row in ledger:
            by_target.setdefault(row["target"], row)
        for target in ("sh", "herdr", "codex", "claude"):
            selected = by_target[target]
            completed = run(
                [
                    executable(binaries["worker"]), "show",
                    "--input", str(accepted),
                    "--input-format", "accepted.instruction",
                    "--events", str(events),
                    "--run", selected["run_id"],
                    "--json",
                ]
            )
            detail = json.loads(completed.stdout)
            if detail["run"]["status"] != "completed" or not detail.get("final"):
                raise AssertionError(f"show readback failed for {target}: {detail}")
            show_rows.append(detail)
        write_jsonl(ARTIFACT / "show-readback.jsonl", show_rows)

        sh_run = by_target["sh"]["run_id"]
        run(
            [
                executable(binaries["worker"]), "tail",
                "--events", str(events),
                "--run", sh_run,
                "--follow=false",
                "--json",
            ],
            stdout_path=ARTIFACT / "tail-readback.jsonl",
            stderr_path=ARTIFACT / "tail-readback.stderr.log",
        )

        target_sessions = {
            target: sorted({row.get("native_session_id") for row in ledger if row["target"] == target and row.get("native_session_id")})
            for target in ("herdr", "codex", "claude")
        }
        expected_sessions = {
            "herdr": ["terminal-proof-1"],
            "codex": ["thread-proof-1"],
            "claude": ["session-proof-1"],
        }
        if target_sessions != expected_sessions:
            raise AssertionError(f"native session readback drift: {target_sessions}")

        write_json(
            ARTIFACT / "readback.json",
            {
                "version": "worker.lane-l-readback.v1",
                "scope_issues": [79, 80, 81, 82, 83, 84],
                "source_instruction_count": len(source_rows),
                "completed_run_count": len(ledger),
                "provider_process_count": len(invocations_before),
                "dry_run_side_effects": 0,
                "duplicate_provider_processes": 0,
                "shell_reparse_side_effects": 0,
                "native_sessions": target_sessions,
                "all_targets": sorted({row["target"] for row in ledger}),
                "fixture_provider": {
                    "fixture_only": True,
                    "external_service_access_claimed": False,
                    "real_binary_process_boundary": True,
                    "real_argv_and_stdin_transport": True,
                    "real_result_and_readback_path": True,
                },
            },
        )
    finally:
        shutil.rmtree(work, ignore_errors=True)


def main() -> None:
    os.chdir(ROOT)
    shutil.rmtree(ARTIFACT, ignore_errors=True)
    ARTIFACT.mkdir(parents=True)

    go_files = [
        "internal/worker/adapter/registry.go",
        "internal/worker/agentadapter/runner.go",
        "internal/worker/agentadapter/sh.go",
        "internal/worker/agentadapter/sh_e2e_test.go",
        "internal/worker/agentadapter/herdr.go",
        "internal/worker/agentadapter/codex.go",
        "internal/worker/agentadapter/claude.go",
        "internal/worker/agentadapter/adapters_test.go",
        "cmd/hq-worker/runtime_registry.go",
        "cmd/hq-worker/runtime_registry_test.go",
        "cmd/hq-worker-proof-provider/main.go",
    ]
    formatting = run(["gofmt", "-d", *go_files])
    write_text(ARTIFACT / "gofmt.diff", formatting.stdout)
    if formatting.stdout.strip():
        raise AssertionError("gofmt diff is not empty")

    run(
        ["go", "test", "./internal/worker/...", "./cmd/hq-worker", "./cmd/hq-worker-proof-provider"],
        stdout_path=ARTIFACT / "go-test.stdout.log",
        stderr_path=ARTIFACT / "go-test.stderr.log",
    )
    if not IS_WINDOWS:
        run(
            ["go", "test", "-race", "./internal/worker/...", "./cmd/hq-worker"],
            stdout_path=ARTIFACT / "go-race.stdout.log",
            stderr_path=ARTIFACT / "go-race.stderr.log",
        )
    run(
        ["go", "vet", "./internal/worker/...", "./cmd/hq-worker", "./cmd/hq-worker-proof-provider"],
        stdout_path=ARTIFACT / "go-vet.stdout.log",
        stderr_path=ARTIFACT / "go-vet.stderr.log",
    )

    contract_proof()
    binaries = build_binaries()
    versions = {
        "version": "worker.lane-l-versions.v1",
        "os": platform.platform(),
        "python": sys.version,
        "go": run(["go", "version"]).stdout.strip(),
        "providers": {
            provider: run([executable(binaries[provider]), "--version"]).stdout.strip()
            for provider in ("sh", "herdr", "codex", "claude")
        },
    }
    write_json(ARTIFACT / "versions.json", versions)
    run_whole_path(binaries)
    write_text(ARTIFACT / "result.txt", "worker lane L whole-path proof passed\n")


if __name__ == "__main__":
    try:
        main()
    except Exception:
        ARTIFACT.mkdir(parents=True, exist_ok=True)
        write_text(ARTIFACT / "failure.txt", traceback.format_exc())
        raise
