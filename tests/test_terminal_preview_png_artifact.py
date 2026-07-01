from pathlib import Path
import os
import subprocess
import sys
import tempfile
import unittest


EXPECTED_STDOUT_BYTES = 18077
EXPECTED_STDOUT_LINES = 254
EXPECTED_STDOUT_BYTE_SUM = 895698
PNG_SIGNATURE = bytes([137, 80, 78, 71, 13, 10, 26, 10])


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
            self.assertEqual(len(stdout_bytes), EXPECTED_STDOUT_BYTES)
            self.assertEqual(len(completed.stdout.splitlines()), EXPECTED_STDOUT_LINES)
            self.assertEqual(sum(stdout_bytes), EXPECTED_STDOUT_BYTE_SUM)
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
            self.assertTrue(data.startswith(PNG_SIGNATURE))
            self.assertGreater(len(data), 1000)
            self.assertEqual(list(Path(tmpdir).glob("*.html")), [])


if __name__ == "__main__":
    unittest.main()
