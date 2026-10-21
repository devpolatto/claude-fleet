"""FLEET-004 com uma pasta de config temporária."""
import importlib.util
import tempfile
import unittest
from pathlib import Path

from sentinela.core import Context

HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location("fleet_004_check", HERE / "check.py")
check = importlib.util.module_from_spec(spec)
spec.loader.exec_module(check)


class PermissionTests(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.cfg = Path(tmp.name) / "claude-fleet"
        self.cfg.mkdir(mode=0o700)
        for name, mode in (("env", 0o600), ("tls.key", 0o600), ("vapid.pem", 0o600), ("tls.crt", 0o644)):
            (self.cfg / name).write_text("x")
            (self.cfg / name).chmod(mode)

    def details(self):
        return [d for _, d in check.check(Context.discover([], config_dir=str(self.cfg)))]

    def test_private_files_are_clean(self):
        self.assertEqual(self.details(), [])

    def test_readable_secret(self):
        (self.cfg / "tls.key").chmod(0o644)
        self.assertEqual(self.details(), ["0o644: devia ser 0o600"])

    def test_group_readable_env(self):
        (self.cfg / "env").chmod(0o640)
        self.assertEqual(len(self.details()), 1)

    def test_writable_dir(self):
        self.cfg.chmod(0o777)
        self.assertIn("outro usuário pode trocar", self.details()[0])

    def test_env_backup(self):
        (self.cfg / "env.bak-20261018").write_text("x")
        (self.cfg / "env.bak-20261018").chmod(0o600)
        self.assertEqual(self.details(), ["cópia do env (tem o token): apague depois de conferir"])

    def test_missing_dir_is_silent(self):
        self.assertEqual([d for _, d in check.check(Context.discover([], config_dir="/nao/existe"))], [])


if __name__ == "__main__":
    unittest.main()
