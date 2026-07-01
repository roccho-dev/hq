import unittest

from hq import JsonlWorld, derive_cursor_context, suggest_keys

SCHEMA = {
    "type": "object",
    "required": ["kind", "title"],
    "properties": {
        "kind": {"type": "string"},
        "title": {"type": "string"},
        "status": {"type": "string"},
    },
}


class SuggestionTests(unittest.TestCase):
    def test_missing_required_keys_are_suggested_first(self):
        world = JsonlWorld.from_schema_and_rows(SCHEMA, [])
        context = derive_cursor_context("{")
        labels = [item.label for item in suggest_keys(world, context)]
        self.assertEqual(labels[:2], ["kind", "title"])

    def test_present_key_is_not_suggested_again(self):
        world = JsonlWorld.from_schema_and_rows(SCHEMA, [])
        context = derive_cursor_context('{"kind":"task",')
        labels = [item.label for item in suggest_keys(world, context)]
        self.assertNotIn("kind", labels)
        self.assertIn("title", labels)

    def test_partial_key_filters_schema_keys_without_unknowns(self):
        world = JsonlWorld.from_schema_and_rows(SCHEMA, [{"unknown": True}])
        context = derive_cursor_context('{"ti')
        labels = [item.label for item in suggest_keys(world, context)]
        self.assertEqual(labels, ["title"])

    def test_suggestion_has_compile_draft(self):
        world = JsonlWorld.from_schema_and_rows(SCHEMA, [])
        context = derive_cursor_context('{"st')
        suggestion = suggest_keys(world, context)[0]
        self.assertEqual(suggestion.label, "status")
        self.assertEqual(suggestion.compileDraft["op"], "set_key")
        self.assertEqual(suggestion.edit["replacePartial"], "st")


if __name__ == "__main__":
    unittest.main()