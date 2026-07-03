import json
from pathlib import Path
import unittest

from tools.hq_reference import JsonlWorld, derive_cursor_context, suggest_keys, suggest_values
from tools.hq_reference.finalize import finalize_selection


FIXTURE = Path("spec/fixtures/protocol-contract.json")


class PythonReferenceProtocolFixtureTests(unittest.TestCase):
    def test_python_reference_consumes_protocol_fixture(self):
        fixture = json.loads(FIXTURE.read_text(encoding="utf-8"))
        world = JsonlWorld.from_jsonl_text(fixture["jsonSchema"], fixture["rowsJSONL"])
        for case in fixture["cases"]:
            with self.subTest(case=case["name"]):
                if case["mode"] == "complete":
                    context = derive_cursor_context(case["buffer"], case["cursor"])
                    self.assertEqual(context.state, case["expectPythonContextState"])
                    self.assertEqual(context.partial, case["expectPartial"])
                    if context.state == "value":
                        suggestions = suggest_values(world, context)
                    else:
                        suggestions = suggest_keys(world, context)
                    labels = [item.label for item in suggestions[: len(case["expectLabels"] )]]
                    self.assertEqual(labels, case["expectLabels"])
                    self.assertTrue(suggestions[0].compileDraft)
                elif case["mode"] == "compile":
                    options = suggest_keys(world, derive_cursor_context("{"))
                    instruction = finalize_selection(options, 0)
                    self.assertEqual(instruction["type"], "jsonl.instruction")
                    self.assertTrue(instruction["compileDraft"])
                else:
                    self.fail(f"unknown fixture mode {case['mode']}")


if __name__ == "__main__":
    unittest.main()
