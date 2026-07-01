from pathlib import Path
import hashlib
import os
import subprocess
import sys
import tempfile
import unittest


EXPECTED_STDOUT_SHA256 = "2f3621c7a272065467ce021da254a7b6af11e88337cbd31d36fe72dd8ce4255d"
EXPECTED_STDOUT_BYTES = 18077
EXPECTED_STDOUT_LINES = 254


class TerminalPreviewPngArtifactTests(unittest.TestCase):
    def test_cli_stdout_and_png_are_generated_from_engine_backed_demo(self):
        env = os.environ.copy()
        env["PYTHONPATH"] = "."
        with tempfile.TemporaryDirectory() as tmpdir:
            stdout_file = Path(tmpdir) / "terminal.txt"
            png_file = Path(tmpdir) / "preview.png"
            completed = subprocess.run(
                [sys.executable, "-m", "hq", "demo-autocomplete"],
                check=True,
                env=env,
                text=True,
                capture_output=True,
            )
            stdout_file.write_text(completed.stdout, encoding="utf-8")
            stdout_bytes = completed.stdout.encode("utf-8")
            self.assertEqual(hashlib.sha256(stdout_bytes).hexdigest(), EXPECTED_STDOUT_SHA256)
            self.assertEqual(len(stdout_bytes), EXPECTED_STDOUT_BYTES)
            self.assertEqual(len(completed.stdout.splitlines()), EXPECTED_STDOUT_LINES)
            self.assertIn("UI 01:", completed.stdout)
            self.assertIn("UI 08:", completed.stdout)
            self.assertIn("source: choose_payload", completed.stdout)
            self.assertIn("unknown_key", completed.stdout)
            self.assertIn("set_value", completed.stdout)
            self.assertNotIn("recent_prs", completed.stdout)
            subprocess.run(
                [sys.executable, "tools/render_terminal_preview_png.py", "--input", str(stdout_file), "--out", str(png_file)],
                check=True,
                env=env,
            )
            data = png_file.read_bytes()
            self.assertTrue(data.startswith(b"\x89PNG\r\n\x1a\n"))
            self.assertGreater(len(data), 1000)
            self.assertEqual(list(Path(tmpdir).glob("*.html")), [])


if __name__ == "__main__":
    unittest.main()
