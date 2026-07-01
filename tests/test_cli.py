import json
import unittest

from hq.cli import build_preview_html, complete_payload, select_payload

SCHEMA_JSON = json.dumps({
    "type": "object",
    "required": ["kind", "title"],
    "properties": {
        "kind": {"type": "string"},
        "title": {"type": "string"},
        "status": {"type": "string"},
    },
})

class CliTests(unittest.TestCase):
    def test_complete_returns_stable_json_ready_payload(self):
        payload = complete_payload(SCHEMA_JSON, "", '{"st')
        self.assertEqual(payload[0]["label"], "status")
        self.assertEqual(payload[0]["compileDraft"]["key"], "status")

    def test_select_returns_final_row(self):
        payload = select_payload(SCHEMA_JSON, "", "{", 0)
        self.assertEqual(payload["type"], "jsonl.instruction")
        self.assertEqual(payload["label"], "kind")

    def test_preview_is_built_from_real_payload(self):
        page = build_preview_html(SCHEMA_JSON, "")
        self.assertIn("hq preview", page)
        self.assertIn("compileDraft", page)
        self.assertIn("status", page)

if __name__ == "__main__":
    unittest.main()
