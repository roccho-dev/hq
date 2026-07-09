import json
import unittest
from collections import defaultdict
from datetime import datetime
from pathlib import Path


FIXTURES = Path("spec/fixtures")
TARGETS = {"sh", "herdr", "codex", "claude"}
STATUSES = {"queued", "running", "completed", "failed", "blocked", "timeout", "cancelled"}
TERMINAL_STATUSES = {"completed", "failed", "blocked", "timeout", "cancelled"}
ALLOWED_TRANSITIONS = {
    ("queued", "running"),
    ("queued", "blocked"),
    ("queued", "cancelled"),
    ("running", "completed"),
    ("running", "failed"),
    ("running", "blocked"),
    ("running", "timeout"),
    ("running", "cancelled"),
}
INSTRUCTION_REQUIRED = {"id", "version", "op", "target", "payload", "created_at"}
INSTRUCTION_OPTIONAL = {"reason", "policy", "reply_to", "labels"}
RESULT_REQUIRED = {
    "event_id",
    "version",
    "run_id",
    "instruction_id",
    "target",
    "kind",
    "seq",
    "recorded_at",
}
RESULT_OPTIONAL = {"message", "final", "error", "native_session_id"}
SESSION_REQUIRED = {
    "version",
    "run_id",
    "instruction_id",
    "target",
    "status",
    "cwd",
    "started_at",
    "last_event_at",
}
SESSION_OPTIONAL = {"native_session_id", "final_path"}
KIND_STATUS = {
    "accepted": "queued",
    "started": "running",
    "completed": "completed",
    "failed": "failed",
    "blocked": "blocked",
    "timeout": "timeout",
    "cancelled": "cancelled",
}


def read_jsonl(path: Path):
    return [json.loads(line) for line in path.read_text().splitlines() if line.strip()]


def is_non_empty_string(value):
    return isinstance(value, str) and bool(value.strip())


def is_rfc3339_utc(value):
    if not is_non_empty_string(value) or not value.endswith("Z"):
        return False
    try:
        datetime.fromisoformat(value[:-1] + "+00:00")
    except ValueError:
        return False
    return True


def validate_instruction(row):
    if not isinstance(row, dict):
        return "invalid_row"
    missing = INSTRUCTION_REQUIRED - row.keys()
    if missing:
        return "missing_required"
    if set(row) - (INSTRUCTION_REQUIRED | INSTRUCTION_OPTIONAL):
        return "unknown_field"
    if not is_non_empty_string(row["id"]):
        return "invalid_id"
    if row["version"] != "instruction.v1":
        return "unknown_version"
    if row["op"] != "run":
        return "unknown_op"
    if row["target"] not in TARGETS:
        return "unknown_target"
    if not is_rfc3339_utc(row["created_at"]):
        return "invalid_created_at"
    for key in ("reason", "reply_to"):
        if key in row and not is_non_empty_string(row[key]):
            return f"invalid_{key}"
    if "labels" in row and (
        not isinstance(row["labels"], list)
        or any(not is_non_empty_string(label) for label in row["labels"])
    ):
        return "invalid_labels"
    if "policy" in row and row["policy"] != {}:
        return "unsupported_policy"

    payload = row["payload"]
    if not isinstance(payload, dict):
        return "invalid_payload"
    if row["target"] == "sh":
        if set(payload) - {"argv", "cwd"}:
            return "invalid_payload"
        argv = payload.get("argv")
        if not isinstance(argv, list) or not argv or any(not is_non_empty_string(item) for item in argv):
            return "invalid_payload"
    else:
        if set(payload) - {"prompt", "cwd"}:
            return "invalid_payload"
        if not is_non_empty_string(payload.get("prompt")):
            return "invalid_payload"
    if "cwd" in payload and not is_non_empty_string(payload["cwd"]):
        return "invalid_payload"
    return None


def validate_instruction_lines(lines):
    valid = []
    errors = []
    seen_ids = set()
    for line_number, line in enumerate(lines, 1):
        try:
            row = json.loads(line)
        except json.JSONDecodeError:
            errors.append((line_number, "malformed_json"))
            continue
        error = validate_instruction(row)
        if error:
            errors.append((line_number, error))
            continue
        if row["id"] in seen_ids:
            errors.append((line_number, "duplicate_id"))
            continue
        seen_ids.add(row["id"])
        valid.append(row)
    return valid, errors


def validate_error(value):
    if not isinstance(value, dict):
        return False
    if set(value) - {"code", "message", "retryable"}:
        return False
    if not is_non_empty_string(value.get("code")) or not is_non_empty_string(value.get("message")):
        return False
    return "retryable" not in value or isinstance(value["retryable"], bool)


def validate_result(row):
    if not isinstance(row, dict):
        return "invalid_row"
    if RESULT_REQUIRED - row.keys():
        return "missing_required"
    if set(row) - (RESULT_REQUIRED | RESULT_OPTIONAL):
        return "unknown_field"
    for key in ("event_id", "run_id", "instruction_id"):
        if not is_non_empty_string(row[key]):
            return f"invalid_{key}"
    if row["version"] != "result.v1":
        return "unknown_version"
    if row["target"] not in TARGETS:
        return "unknown_target"
    if row["kind"] not in {"accepted", "started", "stdout", "stderr", "completed", "failed", "blocked", "timeout", "cancelled"}:
        return "unknown_kind"
    if not isinstance(row["seq"], int) or isinstance(row["seq"], bool) or row["seq"] < 0:
        return "invalid_seq"
    if not is_rfc3339_utc(row["recorded_at"]):
        return "invalid_recorded_at"
    if "native_session_id" in row and not is_non_empty_string(row["native_session_id"]):
        return "invalid_native_session_id"

    kind = row["kind"]
    if kind in {"stdout", "stderr"}:
        if not isinstance(row.get("message"), str):
            return "missing_message"
    elif "message" in row:
        return "unexpected_message"

    if kind == "completed":
        final = row.get("final")
        if not isinstance(final, dict) or set(final) - {"text", "path"}:
            return "invalid_final"
        if not any(is_non_empty_string(final.get(key)) for key in ("text", "path")):
            return "invalid_final"
    elif "final" in row:
        return "unexpected_final"

    if kind in {"failed", "blocked", "timeout", "cancelled"}:
        if not validate_error(row.get("error")):
            return "invalid_error"
    elif "error" in row:
        return "unexpected_error"
    return None


def project_status(rows):
    current = None
    terminal_seen = False
    native_session_id = None
    for expected_seq, row in enumerate(rows):
        if row["seq"] != expected_seq:
            raise AssertionError(f"non-contiguous seq for {row['run_id']}")
        if terminal_seen:
            raise AssertionError(f"event after terminal for {row['run_id']}")
        if row["kind"] == "accepted":
            if expected_seq != 0 or current is not None:
                raise AssertionError(f"accepted must be first for {row['run_id']}")
            next_status = "queued"
        elif row["kind"] in {"stdout", "stderr"}:
            if current != "running":
                raise AssertionError(f"stream event outside running for {row['run_id']}")
            next_status = current
        else:
            next_status = KIND_STATUS[row["kind"]]
            if (current, next_status) not in ALLOWED_TRANSITIONS:
                raise AssertionError(f"invalid transition {current}->{next_status} for {row['run_id']}")
        current = next_status
        if current in TERMINAL_STATUSES:
            terminal_seen = True
        if "native_session_id" in row:
            if native_session_id is not None and native_session_id != row["native_session_id"]:
                raise AssertionError(f"native session changed for {row['run_id']}")
            native_session_id = row["native_session_id"]
    return current, native_session_id


def project_sessions(instructions, result_rows):
    by_instruction = {row["id"]: row for row in instructions}
    by_run = defaultdict(list)
    for row in result_rows:
        by_run[row["run_id"]].append(row)
    projected = []
    for run_id, rows in by_run.items():
        rows.sort(key=lambda row: row["seq"])
        first = rows[0]
        if any(row["instruction_id"] != first["instruction_id"] or row["target"] != first["target"] for row in rows):
            raise AssertionError(f"identity drift for {run_id}")
        instruction = by_instruction[first["instruction_id"]]
        status, native_session_id = project_status(rows)
        session = {
            "version": "session.v1",
            "run_id": run_id,
            "instruction_id": first["instruction_id"],
            "target": first["target"],
            "status": status,
            "cwd": instruction["payload"].get("cwd", "."),
            "started_at": rows[0]["recorded_at"],
            "last_event_at": rows[-1]["recorded_at"],
        }
        if native_session_id:
            session["native_session_id"] = native_session_id
        terminal = rows[-1]
        if terminal["kind"] == "completed" and is_non_empty_string(terminal["final"].get("path")):
            session["final_path"] = terminal["final"]["path"]
        projected.append(session)
    return sorted(projected, key=lambda row: row["run_id"])


def validate_session(row):
    if not isinstance(row, dict):
        return "invalid_row"
    if SESSION_REQUIRED - row.keys():
        return "missing_required"
    if set(row) - (SESSION_REQUIRED | SESSION_OPTIONAL):
        return "unknown_field"
    if row["version"] != "session.v1":
        return "unknown_version"
    for key in ("run_id", "instruction_id", "cwd"):
        if not is_non_empty_string(row[key]):
            return f"invalid_{key}"
    if row["target"] not in TARGETS:
        return "unknown_target"
    if row["status"] not in STATUSES:
        return "unknown_status"
    if not is_rfc3339_utc(row["started_at"]) or not is_rfc3339_utc(row["last_event_at"]):
        return "invalid_time"
    for key in SESSION_OPTIONAL:
        if key in row and not is_non_empty_string(row[key]):
            return f"invalid_{key}"
    return None


class ProtocolContractTest(unittest.TestCase):
    def test_existing_protocol_contract_fixture_is_queue_based(self):
        rows = read_jsonl(FIXTURES / "protocol.contract.jsonl")
        self.assertTrue(rows)
        self.assertTrue(all(row["expectedQueue"] == "instruction.jsonl" for row in rows))
        self.assertEqual({row["case"] for row in rows}, {"key", "enum"})

    def test_instruction_examples_are_valid_and_target_complete(self):
        examples = read_jsonl(FIXTURES / "instruction.examples.jsonl")
        rows = [example["row"] for example in examples]
        self.assertEqual({row["target"] for row in rows}, TARGETS)
        self.assertEqual(len({row["id"] for row in rows}), len(rows))
        for example in examples:
            self.assertIsNone(validate_instruction(example["row"]), example["case"])
            self.assertEqual(example["expected"]["result"]["instruction_id"], example["row"]["id"])
            self.assertEqual(example["expected"]["session"]["instruction_id"], example["row"]["id"])
            self.assertEqual(example["expected"]["result"]["run_id"], example["expected"]["session"]["run_id"])

    def test_invalid_instruction_fixtures_fail_closed_and_continue(self):
        fixtures = read_jsonl(FIXTURES / "instruction.invalid.jsonl")
        for fixture in fixtures:
            valid, errors = validate_instruction_lines(fixture["input_lines"])
            self.assertEqual([code for _, code in errors], fixture["expected_codes"], fixture["case"])
            self.assertEqual(len(valid), fixture["expected_valid_count"], fixture["case"])

    def test_result_rows_reconstruct_runs_and_preserve_final_or_error(self):
        rows = read_jsonl(FIXTURES / "result.runs.jsonl")
        self.assertEqual(len({row["event_id"] for row in rows}), len(rows))
        by_run = defaultdict(list)
        for row in rows:
            self.assertIsNone(validate_result(row), row["event_id"])
            by_run[row["run_id"]].append(row)
        statuses = set()
        for run_rows in by_run.values():
            run_rows.sort(key=lambda row: row["seq"])
            status, _ = project_status(run_rows)
            statuses.add(status)
            terminal = run_rows[-1]
            if status == "completed":
                self.assertIn("final", terminal)
            if status in {"failed", "blocked", "timeout", "cancelled"}:
                self.assertIn("error", terminal)
        self.assertEqual(statuses, STATUSES)

    def test_session_fixture_is_exact_projection(self):
        instructions = [example["row"] for example in read_jsonl(FIXTURES / "instruction.examples.jsonl")]
        results = read_jsonl(FIXTURES / "result.runs.jsonl")
        expected = sorted(read_jsonl(FIXTURES / "session.index.jsonl"), key=lambda row: row["run_id"])
        for row in expected:
            self.assertIsNone(validate_session(row), row["run_id"])
        self.assertEqual(project_sessions(instructions, results), expected)

    def test_status_transition_fixture_is_complete_and_terminal_states_do_not_reopen(self):
        actual = {(row["from"], row["to"]) for row in read_jsonl(FIXTURES / "status.transitions.jsonl")}
        self.assertEqual(actual, ALLOWED_TRANSITIONS)
        self.assertFalse(any(source in TERMINAL_STATUSES for source, _ in actual))
        self.assertFalse(any(target not in STATUSES for _, target in actual))

    def test_contract_docs_keep_execution_and_projection_boundaries_explicit(self):
        instruction_doc = Path("spec/instruction/v1.md").read_text()
        session_doc = Path("spec/session/v1.md").read_text()
        status_doc = Path("spec/status/v1.md").read_text()
        self.assertIn("does not execute", instruction_doc)
        self.assertIn("append-only", instruction_doc)
        self.assertIn("replaceable run-index projection", session_doc)
        self.assertIn("new instruction/run", status_doc)


if __name__ == "__main__":
    unittest.main()
