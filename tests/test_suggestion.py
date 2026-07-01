import unittest

from hq import JsonlWorld, derive_cursor_context, diagnose_keys, suggest_keys, suggest_values

SCHEMA = {
    "type": "object",
    "required": ["kind", "title"],
    "properties": {
        "kind": {"type": "string"},
        "title": {"type": "string"},
        "status": {"type": "string", "enum": ["done", "draft", "deferred"]},
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
        self.assertEqual(suggestion.compileDraft["op"], "complete_key")
        self.assertEqual(suggestion.edit["replacePartial"], "st")

    def test_enum_value_suggestions_are_engine_backed(self):
        world = JsonlWorld.from_schema_and_rows(SCHEMA, [])
        context = derive_cursor_context('{"status": "d')
        suggestions = suggest_values(world, context)
        self.assertEqual([item.label for item in suggestions], ["done", "draft", "deferred"])
        self.assertEqual(suggestions[0].compileDraft["op"], "set_value")

    def test_duplicate_and_unknown_key_diagnostics_have_fixes(self):
        world = JsonlWorld.from_schema_and_rows(SCHEMA, [])
        duplicate = diagnose_keys(world, derive_cursor_context('{"kind":"task","kind'))
        self.assertEqual(duplicate[0].code, "duplicate_key")
        self.assertTrue(duplicate[0].fixes)

        unknown = diagnose_keys(world, derive_cursor_context('{"statuz": "done"'))
        self.assertEqual(unknown[0].code, "unknown_key")
        self.assertEqual(unknown[0].fixes[0].compileDraft["op"], "rename_key")


if __name__ == "__main__":
    unittest.main()
