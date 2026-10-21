"""FLEET-002 com um repo git de mentira e "binários" que só carregam o carimbo do go build."""
import importlib.util
import subprocess
import tempfile
import unittest
from pathlib import Path

from sentinela.core import Context

HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location("fleet_002_check", HERE / "check.py")
check = importlib.util.module_from_spec(spec)
spec.loader.exec_module(check)


class BinaryStampTests(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.root = Path(tmp.name)
        self.repo = self.root / "repo"
        (self.repo / "internal").mkdir(parents=True)
        self.git("init", "-q", "-b", "main")
        self.commit("internal/a.go", "package a\n")
        self.first = self.git("rev-parse", "HEAD")

    def git(self, *args):
        return subprocess.run(["git", "-C", str(self.repo), "-c", "user.email=t@t", "-c", "user.name=t", *args],
                              capture_output=True, text=True, check=True).stdout.strip()

    def commit(self, path, text):
        (self.repo / path).parent.mkdir(parents=True, exist_ok=True)
        (self.repo / path).write_text(text)
        self.git("add", "-A")
        self.git("commit", "-q", "-m", path)

    def binary(self, rev, modified=False):
        path = self.root / "claude-fleet"
        path.write_bytes(b"\x7fELF...build\tvcs.revision=" + rev.encode() +
                         b"\nbuild\tvcs.modified=" + (b"true" if modified else b"false") + b"\n")
        return path

    def run_check(self, binary):
        ctx = Context.discover([], project=self.root, binary=str(binary), repo=str(self.repo), ref="main")
        return [d for _, d in check.check(ctx)]

    def test_current_binary_is_clean(self):
        self.assertEqual(self.run_check(self.binary(self.first)), [])

    def test_docs_only_change_is_clean(self):
        self.commit("README.md", "docs\n")
        self.assertEqual(self.run_check(self.binary(self.first)), [])

    def test_source_change_is_reported(self):
        self.commit("internal/a.go", "package a // corrigido\n")
        details = self.run_check(self.binary(self.first))
        self.assertEqual(len(details), 1)
        self.assertIn(f"compilado de {self.first[:7]}", details[0])

    def test_dirty_build_is_reported(self):
        self.assertIn("vcs.modified=true", self.run_check(self.binary(self.first, modified=True))[0])

    def test_unknown_commit(self):
        self.assertIn("não existe no repo", self.run_check(self.binary("f" * 40))[0])

    def test_missing_stamp(self):
        path = self.root / "sem-carimbo"
        path.write_bytes(b"\x7fELF")
        self.assertIn("sem vcs.revision", self.run_check(path)[0])


if __name__ == "__main__":
    unittest.main()
