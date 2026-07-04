import subprocess
import unittest
from pathlib import Path


class OfficialHQRuntimeTest(unittest.TestCase):
    def test_official_hq_local_check(self):
        subprocess.run(["bash", "scripts/check.sh"], check=True)

    def test_official_hq_linux_tab_proof(self):
        artifacts = Path("artifacts")
        artifacts.mkdir(exist_ok=True)
        binary = artifacts / "hq-linux-amd64"
        proof = artifacts / "official-hq-interactive-tab-proof-linux.txt"

        subprocess.run(["go", "build", "-o", str(binary), "./cmd/hq"], check=True)
        subprocess.run(["python3", "scripts/pty-tab-proof-v2.py", str(binary), str(proof)], check=True)

        text = proof.read_text(encoding="utf-8")
        self.assertIn("INTERACTIVE_TAB_COMPLETION_PROOF: passed", text)
        self.assertIn("SENT_KEYS: {\" + <TAB>", text)
        self.assertIn("SENT_BYTES_HEX: 7b2209", text)
        self.assertIn('"tabProofA"', text)
        self.assertIn('"tabProofB"', text)
        self.assertIn('"tabProofC"', text)


if __name__ == "__main__":
    unittest.main()
