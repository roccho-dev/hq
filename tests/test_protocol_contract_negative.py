import json
import unittest
from pathlib import Path

from test_protocol_contract import (
    ALLOWED_TRANSITIONS,
    STATUSES,
    TARGETS,
    TERMINAL_STATUSES,
    validate_session,
)


FIXTURES = Path("spec/fixtures")
VALIDATION_REQUIRED = {"version", "source_line", "status", "error"}
VALIDATION_OPTIONAL = {"instruction_id", "target"}


def read_jsonl(path: Path):
    return [json.loads(line) for line in path.read_text().splitlines() if line.strip()]


def is_non_empty_string(value):
    return isinstance(value, str) and bool(value.strip())


def validate_validation(row):
    if not isinstance(row, dict):
        return "invalid_row"
    if VALIDATION_REQUIRED - row.keys():
        return "missing_required"
    if set(row) - (VALIDATION_REQUIRED | VALIDATION_OPTIONAL):
        return "unknown_field"
    if row["version"] != "validation.v1":
        return "unknown_version"
    if not isinstance(row["source_line"], int) or isinstance(row["source_line"], bool) or row["source_line"] < 1:
        return "invalid_source_line"
    if row["status"] != "blocked":
        return "invalid_status"
    error = row["error"]
    if not isinstance(error, dict) or set(error) - {"code", "message", "retryable"}:
        return "invalid_error"
    if not is_non_empty_string(error.get("code")) or not is_non_empty_string(error.get("message")):
        return "invalid_error"
    if "retryable" in error and not isinstance(error["retryable"], bool):
        return "invalid_error"
    if "instruction_id" in row and not is_non_empty_string(row["instruction_id"]):
        return "invalid_instruction_id"
    if "target" in row and row["target"] not in TARGETS:
        return "unknown_target"
    return None


class ProtocolContractNegativeClosureTest(unittest.TestCase):
    def test_every_invalid_instruction_has_a_blocked_validation_row(self):
        invalid = {fixture["case"]: fixture for fixture in read_jsonl(FIXTURES / "instruction.invalid.jsonl")}
        rejections = {fixture["case"]: fixture["row"] for fixture in read_jsonl(FIXTURES / "validation.rejections.jsonl")}
        self.assertEqual(set(rejections), set(invalid))
        for case, row in rejections.items():
            self.assertIsNone(validate_validation(row), case)
            self.assertIn(row["error"]["code"], invalid[case]["expected_codes"], case)
        self.assertEqual(rejections["duplicate-id"]["source_line"], 2)
        self.assertEqual(rejections["malformed-json-followed-by-valid"]["source_line"], 1)
        self.assertEqual(invalid["malformed-json-followed-by-valid"]["expected_valid_count"], 1)

    def test_unknown_status_and_illegal_transition_fixtures_fail(self):
        fixtures = read_jsonl(FIXTURES / "status.invalid.jsonl")
        self.assertTrue(fixtures)
        for fixture in fixtures:
            expected = fixture["expected_code"]
            if "session" in fixture:
                self.assertEqual(validate_session(fixture["session"]), expected, fixture["case"])
                continue
            source = fixture["transition"]["from"]
            target = fixture["transition"]["to"]
            self.assertNotIn((source, target), ALLOWED_TRANSITIONS, fixture["case"])
            if expected == "unknown_status":
                self.assertTrue(source not in STATUSES or target not in STATUSES, fixture["case"])
            elif expected == "terminal_reopen":
                self.assertIn(source, TERMINAL_STATUSES, fixture["case"])
            else:
                self.assertEqual(expected, "invalid_transition", fixture["case"])
                self.assertIn(source, STATUSES, fixture["case"])
                self.assertIn(target, STATUSES, fixture["case"])

    def test_validation_contract_keeps_invalid_input_out_of_run_projection(self):
        document = Path("spec/validation/v1.md").read_text()
        self.assertIn("separate from `result.v1`", document)
        self.assertIn("never create a session projection", document)
        self.assertIn("continues reading later lines", document)


if __name__ == "__main__":
    unittest.main()
