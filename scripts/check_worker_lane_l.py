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
WINDOWS = os.name == "nt"
EXE = ".exe" if WINDOWS else ""


def write_text(path: Path, text: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(text, encoding="utf-8")


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


def executable(path: Path) -> str:
    return str(path.resolve())


def digest(path: Path) -> str:
    return "sha256:" + hashlib.sha256(path.read_bytes()).hexdigest()


def build_binaries() -> dict[str, Path]:
    output = ARTIFACT / "bin"
    output.mkdir(parents=True, exist_ok=True)
    binaries = {
        "hq": output / f"hq{EXE}",
        "worker": output / f"hq-worker{EXE}",
        "provider": output / f"hq-worker-proof-provider{EXE}",
    }
    run(["go", "build", "-o", str(binaries["hq"]), "./cmd/hq"])
    run(["go", "build", "-o", str(binaries["worker"]), "./cmd/hq-worker"])
    run(["go", "build", "-o", str(binaries["provider"]), "./cmd/hq-worker-proof-provider"])
    for name in ("sh", "herdr", "codex", "claude"):
        target = output / f"proof-{name}{EXE}"
        shutil.copy2(binaries["provider"], target)
        if not WINDOWS:
            target.chmod(0o755)
        binaries[name] = target
    return binaries


def contract_proof() -> None:
    run(
        [sys.executable, "-m", "unittest", "-v", "tests/test_protocol_contract.py"],
        stdout_path=ARTIFACT / "contract-fixture-test.stdout.log",
        stderr_path=ARTIFACT / "contract-fixture-test.stderr.log",
    )
    fixtures = [
        "spec/fixtures/instruction.examples.jsonl",
        "spec/fixtures/instruction.invalid.jsonl",
        "spec/fixtures/result.runs.jsonl",
        "spec/fixtures/session.index.jsonl",
        "spec/fixtures/status.transitions.jsonl",
        "spec/fixtures/agent-adapters/instructions.jsonl",
    ]
    write_json(
        ARTIFACT / "contract-fixture-matrix.json",
        {
            "version": "worker.contract-fixture-matrix.v1",
            "valid_fixtures_pass": True,
            "invalid_fixtures_fail_closed": True,
            "fixtures": [
                {
                    "path": name,
                    "rows": len(read_jsonl(ROOT / name)),
                    "sha256": digest(ROOT / name),
                    "valid_or_negative": "negative" if "invalid" in name else "valid",
                }
                for name in fixtures
            ],
        },
    )


def instruction(
    identifier: str,
    target: str,
    payload: dict[str, Any],
    minute: int,
    **optional: Any,
) -> dict[str, Any]:
    row = {
        "id": identifier,
        "version": "instruction.v1",
        "op": "run",
        "target": target,
        "payload": payload,
        "created_at": f"2026-07-10T07:{minute:02d}:00Z",
    }
    row.update(optional)
    return row


def source_rows(binaries: dict[str, Path], marker: Path) -> list[dict[str, Any]]:
    literal = f"; touch {marker} && echo $(uname)"
    return [
        instruction(
            "lane-l-sh-001",
            "sh",
            {"cwd": ".", "argv": [executable(binaries["sh"]), "echo", literal]},
            20,
            reason="lane L direct argv and injection proof",
            labels=["lane-l", "fixture-proof"],
        ),
        instruction(
            "lane-l-herdr-start-001",
            "herdr",
            {
                "action": "start",
                "cwd": ".",
                "name": "lane-l-proof",
                "no_focus": True,
                "split": "right",
                "command_argv": [executable(binaries["claude"]), "-p"],
                "prompt": "Return deterministic proof.",
            },
            21,
        ),
        instruction(
            "lane-l-herdr-read-001",
            "herdr",
            {
                "action": "read",
                "cwd": ".",
                "agent": "terminal-proof-1",
                "source": "recent-unwrapped",
                "lines": 100,
            },
            22,
        ),
        instruction(
            "lane-l-herdr-observe-001",
            "herdr",
            {
                "action": "observe",
                "cwd": ".",
                "agent": "terminal-proof-1",
                "wait_status": "idle",
                "timeout_ms": 1000,
                "lines": 100,
            },
            23,
        ),
        instruction(
            "lane-l-herdr-attach-001",
            "herdr",
            {"action": "attach", "cwd": ".", "agent": "terminal-proof-1", "takeover": False},
            24,
        ),
        instruction(
            "lane-l-codex-exec-001",
            "codex",
            {
                "action": "exec",
                "prompt": "Return Codex lane L proof.",
                "cwd": ".",
                "output_path": ".hq/final/codex-exec.txt",
                "sandbox": "workspace-write",
                "skip_git_repo_check": True,
            },
            25,
        ),
        instruction(
            "lane-l-codex-resume-001",
            "codex",
            {
                "action": "resume",
                "prompt": "Continue Codex lane L proof.",
                "session_id": "thread-proof-1",
                "cwd": ".",
                "output_path": ".hq/final/codex-resume.txt",
                "sandbox": "workspace-write",
                "skip_git_repo_check": True,
            },
            26,
            reply_to="lane-l-codex-exec-001",
        ),
        instruction(
            "lane-l-claude-print-001",
            "claude",
            {
                "action": "print",
                "prompt": "Return Claude lane L proof.",
                "cwd": ".",
                "output_format": "json",
                "max_turns": 1,
            },
            27,
        ),
        instruction(
            "lane-l-claude-resume-001",
            "claude",
            {
                "action": "resume",
                "prompt": "Continue Claude lane L proof.",
                "session_id": "session-proof-1",
                "cwd": ".",
                "output_format": "stream-json",
                "max_turns": 1,
            },
            28,
            reply_to="lane-l-claude-print-001",
        ),
    ]


def accept_all(hq: Path, rows: list[dict[str, Any]], queue: Path) -> None:
    outputs = []
    for row in rows:
        result = run(
            [
                executable(hq),
                "--accept",
                json.dumps(row, ensure_ascii=False, separators=(",", ":")),
                "--queue",
                str(queue),
            ]
        )
        outputs.append(json.loads(result.stdout))
    write_jsonl(ARTIFACT / "accepted-command-output.jsonl", outputs)
    shutil.copy2(queue, ARTIFACT / "accepted-instructions.jsonl")


def approvals(plan: list[dict[str, Any]], path: Path) -> None:
    rows = [
        {
            "version": "worker.approval.v1",
            "instruction_id": row["instruction_id"],
            "approved": True,
            "approved_by": "lane-l-ci",
            "instruction_digest": row["instruction_digest"],
        }
        for row in plan
    ]
    write_jsonl(path, rows)
    shutil.copy2(path, ARTIFACT / "approvals.jsonl")


def session_projection(ledger: list[dict[str, Any]]) -> list[dict[str, Any]]:
    projected = []
    for row in ledger:
        value = {
            "version": "session.v1",
            "run_id": row["run_id"],
            "instruction_id": row["instruction_id"],
            "target": row["target"],
            "status": row["status"],
            "cwd": row["cwd"],
            "started_at": row["started_at"],
            "last_event_at": row["last_event_at"],
        }
        for key in ("native_session_id", "final_path"):
            if row.get(key):
                value[key] = row[key]
        projected.append(value)
    return projected


def verify_invocations(rows: list[dict[str, Any]], marker: Path) -> None:
    if len(rows) != 10:
        raise AssertionError(f"unexpected provider process count: {len(rows)}")
    if any(row.get("fixture_only") is not True or not row.get("argv") for row in rows):
        raise AssertionError("provider invocation is not inspectable")

    sh_rows = [row for row in rows if row["provider"] == "sh"]
    if len(sh_rows) != 1 or sh_rows[0]["argv"] != ["echo", f"; touch {marker} && echo $(uname)"]:
        raise AssertionError(f"sh argv drift: {sh_rows}")

    for provider in ("codex", "claude"):
        provider_rows = [row for row in rows if row["provider"] == provider]
        if len(provider_rows) != 2 or any(row["stdin_bytes"] <= 0 for row in provider_rows):
            raise AssertionError(f"{provider} stdin proof is missing")
    if any(row["argv"][-1] != "-" for row in rows if row["provider"] == "codex"):
        raise AssertionError("Codex did not use the stdin marker")
    if any("-p" not in row["argv"] for row in rows if row["provider"] == "claude"):
        raise AssertionError("Claude did not use print mode")


def whole_path(binaries: dict[str, Path]) -> None:
    work = Path(tempfile.mkdtemp(prefix="hq-worker-lane-l-"))
    try:
        project = work / "project"
        project.mkdir(parents=True)
        marker = project / "injection-must-not-exist"
        source = source_rows(binaries, marker)
        write_jsonl(ARTIFACT / "source-instructions.jsonl", source)

        queue = work / "accepted.jsonl"
        accept_all(binaries["hq"], source, queue)
        events = work / "events.jsonl"
        provider_log = ARTIFACT / "provider-invocations.jsonl"

        dry = run(
            [
                executable(binaries["worker"]),
                "--input", str(queue),
                "--input-format", "accepted.instruction",
                "--events", str(events),
                "--workspace", str(project),
                "--dry-run",
            ],
            stdout_path=ARTIFACT / "dry-run-plan.jsonl",
            stderr_path=ARTIFACT / "dry-run.stderr.log",
        )
        plan = [json.loads(line) for line in dry.stdout.splitlines() if line.strip()]
        if len(plan) != len(source) or any(row["decision"] != "accepted" for row in plan):
            raise AssertionError("accepted dry-run plan is incomplete")
        if events.exists() or provider_log.exists() or marker.exists():
            raise AssertionError("dry-run caused a side effect")

        invalid_path = work / "invalid.jsonl"
        write_jsonl(
            invalid_path,
            [
                instruction("lane-l-invalid-target", "unknown", {}, 29),
                instruction("lane-l-invalid-argv", "sh", {"argv": []}, 30),
            ],
        )
        invalid = run(
            [
                executable(binaries["worker"]),
                "--input", str(invalid_path),
                "--input-format", "instruction.v1",
                "--events", str(events),
                "--workspace", str(project),
                "--dry-run",
            ],
            expected=(2,),
            stdout_path=ARTIFACT / "dry-run-invalid-plan.jsonl",
            stderr_path=ARTIFACT / "dry-run-invalid.stderr.log",
        )
        invalid_plan = [json.loads(line) for line in invalid.stdout.splitlines() if line.strip()]
        if len(invalid_plan) != 2 or any(
            row["decision"] != "blocked" or not row.get("validation_errors") for row in invalid_plan
        ):
            raise AssertionError(f"invalid dry-run did not fail closed: {invalid_plan}")
        if events.exists() or provider_log.exists() or marker.exists():
            raise AssertionError("invalid dry-run caused a side effect")

        approval_path = work / "approvals.jsonl"
        approvals(plan, approval_path)
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
                "--input", str(queue),
                "--input-format", "accepted.instruction",
                "--events", str(events),
                "--approvals", str(approval_path),
                "--workspace", str(project),
            ],
            env=environment,
            stdout_path=ARTIFACT / "execution-output.jsonl",
            stderr_path=ARTIFACT / "execution.stderr.log",
        )
        if marker.exists():
            raise AssertionError("metacharacter argv was reparsed by a shell")

        event_rows = read_jsonl(events)
        results = [row for row in event_rows if row.get("version") == "result.v1"]
        grouped: dict[str, list[dict[str, Any]]] = defaultdict(list)
        for row in results:
            grouped[row["instruction_id"]].append(row)
        if set(grouped) != {row["id"] for row in source}:
            raise AssertionError("not every accepted instruction has result evidence")
        for identifier, rows in grouped.items():
            rows.sort(key=lambda row: row["seq"])
            if rows[-1]["kind"] != "completed" or not rows[-1].get("final"):
                raise AssertionError(f"{identifier} lacks durable completion: {rows[-1]}")

        invocations = read_jsonl(provider_log)
        verify_invocations(invocations, marker)
        before_duplicate = len(invocations)
        duplicate = run(
            [
                executable(binaries["worker"]),
                "--input", str(queue),
                "--input-format", "accepted.instruction",
                "--events", str(events),
                "--approvals", str(approval_path),
                "--workspace", str(project),
            ],
            expected=(2,),
            env=environment,
            stdout_path=ARTIFACT / "duplicate-read-output.jsonl",
            stderr_path=ARTIFACT / "duplicate-read.stderr.log",
        )
        duplicate_rows = [json.loads(line) for line in duplicate.stdout.splitlines() if line.strip()]
        if len(duplicate_rows) != len(source) or any(
            row.get("version") != "validation.v1" or row.get("error", {}).get("code") != "duplicate_id"
            for row in duplicate_rows
        ):
            raise AssertionError("duplicate reread did not fail closed")
        if len(read_jsonl(provider_log)) != before_duplicate:
            raise AssertionError("duplicate reread executed a provider")

        shutil.copy2(events, ARTIFACT / "events.jsonl")
        ledger_result = run(
            [
                executable(binaries["worker"]), "list",
                "--input", str(queue),
                "--input-format", "accepted.instruction",
                "--events", str(events),
                "--limit", "0",
                "--json",
            ],
            stdout_path=ARTIFACT / "ledger.jsonl",
            stderr_path=ARTIFACT / "ledger.stderr.log",
        )
        ledger = [json.loads(line) for line in ledger_result.stdout.splitlines() if line.strip()]
        if len(ledger) != len(source) or any(row["status"] != "completed" for row in ledger):
            raise AssertionError("ledger readback is incomplete")
        write_jsonl(ARTIFACT / "session-projection.jsonl", session_projection(ledger))

        by_target: dict[str, dict[str, Any]] = {}
        for row in ledger:
            by_target.setdefault(row["target"], row)
        show_rows = []
        for target in ("sh", "herdr", "codex", "claude"):
            detail = json.loads(
                run(
                    [
                        executable(binaries["worker"]), "show",
                        "--input", str(queue),
                        "--input-format", "accepted.instruction",
                        "--events", str(events),
                        "--run", by_target[target]["run_id"],
                        "--json",
                    ]
                ).stdout
            )
            if detail["run"]["status"] != "completed" or not detail.get("final"):
                raise AssertionError(f"show readback failed for {target}")
            show_rows.append(detail)
        write_jsonl(ARTIFACT / "show-readback.jsonl", show_rows)
        run(
            [
                executable(binaries["worker"]), "tail",
                "--events", str(events),
                "--run", by_target["sh"]["run_id"],
                "--follow=false",
                "--json",
            ],
            stdout_path=ARTIFACT / "tail-readback.jsonl",
            stderr_path=ARTIFACT / "tail-readback.stderr.log",
        )

        expected_sessions = {
            "herdr": ["terminal-proof-1"],
            "codex": ["thread-proof-1"],
            "claude": ["session-proof-1"],
        }
        actual_sessions = {
            target: sorted(
                {
                    row["native_session_id"]
                    for row in ledger
                    if row["target"] == target and row.get("native_session_id")
                }
            )
            for target in expected_sessions
        }
        if actual_sessions != expected_sessions:
            raise AssertionError(f"native session readback drift: {actual_sessions}")

        final_source = project / ".hq" / "final"
        final_manifest = []
        if final_source.exists():
            final_artifact = ARTIFACT / "final"
            shutil.copytree(final_source, final_artifact)
            final_manifest = [
                {
                    "path": str(path.relative_to(ARTIFACT)).replace("\\", "/"),
                    "sha256": digest(path),
                }
                for path in sorted(final_artifact.rglob("*"))
                if path.is_file()
            ]
        write_json(ARTIFACT / "final-manifest.json", {"version": "worker.final-manifest.v1", "files": final_manifest})
        write_json(
            ARTIFACT / "readback.json",
            {
                "version": "worker.lane-l-readback.v1",
                "scope_issues": [79, 80, 81, 82, 83, 84],
                "source_instruction_count": len(source),
                "completed_run_count": len(ledger),
                "provider_process_count": len(invocations),
                "dry_run_side_effects": 0,
                "duplicate_provider_processes": 0,
                "shell_reparse_side_effects": 0,
                "native_sessions": actual_sessions,
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
    if WINDOWS:
        write_text(ARTIFACT / "gofmt.diff", "checked by the Linux matrix job\n")
    else:
        formatted = run(["gofmt", "-d", *go_files])
        write_text(ARTIFACT / "gofmt.diff", formatted.stdout)
        if formatted.stdout.strip():
            raise AssertionError("gofmt diff is not empty")

    run(
        ["go", "test", "./internal/worker/...", "./cmd/hq-worker", "./cmd/hq-worker-proof-provider"],
        stdout_path=ARTIFACT / "go-test.stdout.log",
        stderr_path=ARTIFACT / "go-test.stderr.log",
    )
    if not WINDOWS:
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
    write_json(
        ARTIFACT / "versions.json",
        {
            "version": "worker.lane-l-versions.v1",
            "os": platform.platform(),
            "python": sys.version,
            "go": run(["go", "version"]).stdout.strip(),
            "providers": {
                name: run([executable(binaries[name]), "--version"]).stdout.strip()
                for name in ("sh", "herdr", "codex", "claude")
            },
        },
    )
    whole_path(binaries)
    write_text(ARTIFACT / "result.txt", "worker lane L whole-path proof passed\n")


if __name__ == "__main__":
    try:
        main()
    except Exception:
        ARTIFACT.mkdir(parents=True, exist_ok=True)
        write_text(ARTIFACT / "failure.txt", traceback.format_exc())
        raise
