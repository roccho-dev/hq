from pathlib import Path
import os
import subprocess
import sys
import tempfile
import unittest


class TerminalPreviewPngArtifactTests(unittest.TestCase):
    def run_cli(self, out: Path) -> str:
        env = os.environ.copy()
        env["PYTHONPATH"] = "."
        completed = subprocess.run(
            [sys.executable, "-m", "hq", "demo-autocomplete"],
            check=True,
            env=env,
            text=True,
            capture_output=True,
        )
        out.write_text(completed.stdout, encoding="utf-8")
        return completed.stdout

    def run_renderer(self, input_text: Path, out: Path) -> None:
        env = os.environ.copy()
        env["PYTHONPATH"] = "."
        subprocess.run(
            [sys.executable, "tools/render_terminal_preview_png.py", "--input", str(input_text), "--out", str(out)],
            check=True,
            env=env,
        )

    def test_actual_cli_output_contains_all_ascii_screens(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            stdout = self.run_cli(Path(tmpdir) / "terminal.txt")
            for index in range(1, 11):
                self.assertIn(f"UI {index:02d}:", stdout)
            self.assertIn("Ctrl-N/P move", stdout)
            self.assertIn("Ctrl-Space details", stdout)
            self.assertIn("op: complete_key", stdout)
            self.assertIn("op: set_value", stdout)
            self.assertIn("op: rename_key", stdout)

    def test_actual_cli_output_uses_highlight_not_number_choice(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            stdout = self.run_cli(Path(tmpdir) / "terminal.txt")
            self.assertNotIn("[0]", stdout)
            self.assertIn("| | >  | status", stdout)
            self.assertIn("| | >  | title", stdout)
            self.assertIn("| > done", stdout)

    def test_png_preview_is_generated_from_actual_cli_stdout(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            stdout_file = Path(tmpdir) / "terminal.txt"
            png_file = Path(tmpdir) / "hq-terminal-autocomplete-preview.png"
            self.run_cli(stdout_file)
            self.run_renderer(stdout_file, png_file)
            data = png_file.read_bytes()
            self.assertTrue(data.startswith(b"\x89PNG\r\n\x1a\n"))
            self.assertGreater(len(data), 1000)

    def test_no_html_preview_is_required(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            stdout_file = Path(tmpdir) / "terminal.txt"
            png_file = Path(tmpdir) / "preview.png"
            self.run_cli(stdout_file)
            self.run_renderer(stdout_file, png_file)
            self.assertEqual(list(Path(tmpdir).glob("*.html")), [])


if __name__ == "__main__":
    unittest.main()
