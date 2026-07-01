import unittest

from hq.terminal_surface import autocomplete_payload, choose_payload

SCHEMA = {
    "type": "object",
    "required": ["status", "title"],
    "properties": {
        "status": {"type": "string", "enum": ["done", "draft", "deferred"]},
        "title": {"type": "string"},
        "assignee": {"type": "string"},
    },
}


class TerminalSurfaceTests(unittest.TestCase):
    def test_surface_uses_real_key_payload(self):
        payload = autocomplete_payload(SCHEMA, "", "{")
        self.assertEqual(payload["context"]["state"], "object_open")
        self.assertEqual(payload["suggestions"][0]["label"], "status")
        self.assertEqual(payload["suggestions"][0]["compileDraft"]["op"], "set_key")

    def test_surface_uses_real_value_payload(self):
        payload = autocomplete_payload(SCHEMA, "", '{"status": "d')
        self.assertEqual([item["label"] for item in payload["suggestions"]], ["done", "draft", "deferred"])

    def test_surface_reports_real_diagnostics(self):
        payload = autocomplete_payload(SCHEMA, "", '{"statuz": "done"')
        self.assertEqual(payload["diagnostics"][0]["code"], "unknown_key")
        self.assertEqual(payload["diagnostics"][0]["fixes"][0]["compileDraft"]["op"], "rename_key")

    def test_choose_payload_uses_same_engine(self):
        row = choose_payload(SCHEMA, "", '{"status": "done", "ti', 0)
        self.assertEqual(row["label"], "title")
        self.assertEqual(row["compileDraft"]["op"], "complete_key")


if __name__ == "__main__":
    unittest.main()
