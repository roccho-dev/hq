from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


class TerminalPreviewPngArtifactTests(unittest.TestCase):
    def test_png_preview_is_generated_from_script(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            out = Path(tmpdir) / "hq-terminal-autocomplete-preview.png"
            subprocess.run(
                [sys.executable, "tools/render_terminal_preview_png.py", "--out", str(out)],
                check=True,
            )
            data = out.read_bytes()
            self.assertTrue(data.startswith(b"\x89PNG\r\n\x1a\n"))
            self.assertGreater(len(data), 1000)

    def test_no_html_preview_is_required(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            out = Path(tmpdir) / "preview.png"
            subprocess.run(
                [sys.executable, "tools/render_terminal_preview_png.py", "--out", str(out)],
                check=True,
            )
            self.assertEqual(list(Path(tmpdir).glob("*.html")), [])


if __name__ == "__main__":
    unittest.main()
