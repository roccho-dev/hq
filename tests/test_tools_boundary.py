from pathlib import Path


def test_python_metadata_is_tooling():
    text = Path("pyproject.toml").read_text(encoding="utf-8")
    assert "hq-python-tools" in text


def test_tools_boundary_is_documented():
    text = Path("tools/README.md").read_text(encoding="utf-8")
    assert "Tooling" in text or "Tooling".lower() in text.lower()
