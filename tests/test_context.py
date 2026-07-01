import unittest

from hq.core import JsonlWorld, derive_cursor_context

SCHEMA = {
    "type": "object",
    "required": ["kind", "title"],
    "properties": {
        "kind": {"type": "string"},
        "title": {"type": "string"},
        "status": {"type": "string"},
    },
}

class JsonlWorldTests(unittest.TestCase):
    def test_world_reads_schema_required_and_existing_keys(self):
        world = JsonlWorld.from_schema_and_rows(SCHEMA, [{"kind": "task", "noise": True}, {"status": "open"}])
        self.assertEqual(world.schema_keys, ("kind", "status", "title"))
        self.assertEqual(world.required_keys, ("kind", "title"))
        self.assertEqual(world.existing_keys, ("kind", "status"))

    def test_world_reads_jsonl_text(self):
        text = '{"kind":"task"}\n{"title":"done"}\n'
        world = JsonlWorld.from_jsonl_text(SCHEMA, text)
        self.assertEqual(world.existing_keys, ("kind", "title"))

class CursorContextTests(unittest.TestCase):
    def test_empty_buffer(self):
        context = derive_cursor_context("")
        self.assertEqual(context.state, "empty")
        self.assertEqual(context.partial, "")

    def test_object_open(self):
        context = derive_cursor_context("{")
        self.assertEqual(context.state, "object_open")
        self.assertEqual(context.present_keys, ())

    def test_partial_key(self):
        context = derive_cursor_context('{"ti')
        self.assertEqual(context.state, "key")
        self.assertEqual(context.partial, "ti")

    def test_next_key_keeps_present_keys(self):
        context = derive_cursor_context('{"kind":"task",')
        self.assertEqual(context.state, "next_key")
        self.assertEqual(context.present_keys, ("kind",))

    def test_value_context(self):
        context = derive_cursor_context('{"kind": "ta')
        self.assertEqual(context.state, "value")
        self.assertEqual(context.partial, "ta")
        self.assertEqual(context.current_key, "kind")

    def test_missing_required_keys_use_world(self):
        world = JsonlWorld.from_schema_and_rows(SCHEMA, [])
        context = derive_cursor_context('{"kind":"task",')
        self.assertEqual(context.missing_required_keys_for(world), ("title",))

if __name__ == "__main__":
    unittest.main()
