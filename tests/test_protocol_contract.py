import json
from pathlib import Path


def test_protocol_contract_fixture_is_queue_based():
    rows = [json.loads(line) for line in Path("spec/fixtures/protocol.contract.jsonl").read_text().splitlines()]
    assert rows
    assert all(row["expectedQueue"] == "instruction.jsonl" for row in rows)
    assert {row["case"] for row in rows} == {"key", "enum"}
