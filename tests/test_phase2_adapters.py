from __future__ import annotations

from pathlib import Path
import unittest

from hq.phase2 import QueueRow, append_if_accepted, effective_projection, encode_queue_row, projected_to_world, read_queue_jsonl

ROOT = Path(__file__).resolve().parents[1]


class Phase2AdaptersTest(unittest.TestCase):
    def test_valid_queue_rows_pass(self) -> None:
        text = (ROOT / "spec/queue/valid.queue.jsonl").read_text(encoding="utf-8")
        rows = read_queue_jsonl(text)
        self.assertEqual(len(rows), 4)
        self.assertEqual(rows[0].source_ref, "adrs.projected:fixed:purpose-1")

    def test_bad_queue_rows_fail(self) -> None:
        text = (ROOT / "spec/queue/bad.queue.jsonl").read_text(encoding="utf-8")
        for line in text.splitlines():
            with self.assertRaises(ValueError):
                read_queue_jsonl(line)

    def test_append_only_writer_respects_acceptance(self) -> None:
        row = QueueRow("q-test", "ui.overlay", "overlay.show", "opaque://source#1", {"title": "Draft"}, "pending", {"by": "hq"}, "2026-07-03T00:00:00Z")
        self.assertEqual(append_if_accepted("", False, row), "")
        written = append_if_accepted("", True, row)
        self.assertEqual(written, encode_queue_row(row) + "\n")
        self.assertEqual(read_queue_jsonl(written)[0].id, "q-test")

    def test_projected_reader_maps_to_world(self) -> None:
        projected = (ROOT / "spec/projected/adrs.projected.fixed.jsonl").read_text(encoding="utf-8")
        world = projected_to_world(projected)
        self.assertIn("purpose", world.schema_keys)
        self.assertIn("proposal", world.schema_keys)
        self.assertEqual(world.required_keys, ("purpose",))
        self.assertEqual(world.rows[0]["source_ref"], "adrs.projected:fixed:purpose-1")

    def test_effective_projection_keeps_fixed_source_unchanged(self) -> None:
        projected_path = ROOT / "spec/projected/adrs.projected.fixed.jsonl"
        queue_path = ROOT / "spec/queue/valid.queue.jsonl"
        before = projected_path.read_text(encoding="utf-8")
        effective = effective_projection(before, queue_path.read_text(encoding="utf-8"))
        after = projected_path.read_text(encoding="utf-8")
        self.assertEqual(before, after)
        queue_rows = [row for row in effective if row["projection_layer"] == "queue"]
        self.assertEqual(len(queue_rows), 4)
        proposal = [row for row in queue_rows if row["target"] == "adrs.proposal"][0]
        self.assertEqual(proposal["payload"]["authority"], "pending")
        overlay = [row for row in queue_rows if row["target"] == "ui.overlay"][0]
        self.assertFalse(overlay["payload"]["state_store"])


if __name__ == "__main__":
    unittest.main()
