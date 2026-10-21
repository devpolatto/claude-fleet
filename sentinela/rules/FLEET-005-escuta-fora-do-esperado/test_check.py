"""FLEET-005 com saída de `ss -ltnpH` de mentira."""
import importlib.util
import tempfile
import unittest
from pathlib import Path

from sentinela.core import Context

HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location("fleet_005_check", HERE / "check.py")
check = importlib.util.module_from_spec(spec)
spec.loader.exec_module(check)

FLEET = 'users:(("claude-fleet",pid=5631,fd=3))'
OTHER = 'users:(("sshd",pid=1,fd=3))'


def line(addr, users=FLEET):
    return f"LISTEN 0      4096      {addr}  0.0.0.0:*  {users}"


class ListenerTests(unittest.TestCase):
    def details(self, *lines):
        with tempfile.NamedTemporaryFile("w", suffix=".txt", delete=False) as f:
            f.write("\n".join(lines) + "\n")
        self.addCleanup(Path(f.name).unlink)
        return [d for _, d in check.check(Context.discover([], ss_output=f.name))]

    def test_production_layout_is_clean(self):
        self.assertEqual(self.details(line("100.64.0.10:7777"), line("127.0.0.1:7777"),
                                      line("[fd7a:115c:a1e0::10]:7777"), line("[::1]:7777")), [])

    def test_all_interfaces(self):
        for addr in ("0.0.0.0:7777", "*:7777", "[::]:7777"):
            with self.subTest(addr=addr):
                self.assertEqual(len(self.details(line(addr))), 1)

    def test_lan_address(self):
        self.assertEqual(len(self.details(line("192.168.20.10:7777"))), 1)

    def test_other_process_is_ignored(self):
        self.assertEqual(self.details(line("0.0.0.0:22", OTHER), line("127.0.0.1:7777")), [])

    def test_not_running(self):
        self.assertIn("processo não encontrado", self.details(line("0.0.0.0:22", OTHER))[0])


if __name__ == "__main__":
    unittest.main()
