import json
import unittest

from hq.core import JsonlWorld, derive_cursor_context
from hq.finalize import append_instruction_jsonl, finalize_selection
from hq.suggestion import suggest_keys

SCHEMA = {
    "type": "object",
    "required": ["kind", "title"],
    "properties": {
        "kind": {"type": "string"},
        "title": {"type": "string"},
        "status": {"type": "string"},
    },
}

class FinalJsonlTests(unittest.TestCase):
    def test_selected_candidate_becomes_row(self):
        world = JsonlWorld.from_schema_and_rows(SCHEMA, [])
        options = suggest_keys(world, derive_cursor_context("{"))
        row = finalize_selection(options, 1)
        self.assertEqual(row["label"], "title")
        self.assertEqual(row["compileDraft"]["key"], "title")

    def test_row_appends_as_jsonl(self):
        world = JsonlWorld.from_schema_and_rows(SCHEMA, [])
        options = suggest_keys(world, derive_cursor_context("{"))
        row = finalize_selection(options, 0)
        output = append_instruction_jsonl("", row)
        parsed = json.loads(output)
        self.assertEqual(parsed["type"], "jsonl.instruction")
        self.assertEqual(parsed["label"], "kind")

if __name__ == "__main__":
    unittest.main()
