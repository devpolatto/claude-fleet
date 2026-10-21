"""FLEET-001 contra servidores falsos em 127.0.0.1: um que segue o corpus e um furado."""
import importlib.util
import threading
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

from sentinela.core import Context

HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location("fleet_001_check", HERE / "check.py")
check = importlib.util.module_from_spec(spec)
spec.loader.exec_module(check)

KEYS = ("Origin", "Sec-Fetch-Site", "Sec-Fetch-Dest", "Content-Type")


def server(answer):
    """Sobe um servidor que responde answer(handler, corpo) e devolve (srv, url)."""
    class H(BaseHTTPRequestHandler):
        def respond(self):
            body = self.rfile.read(int(self.headers.get("Content-Length") or 0))
            self.send_response(answer(self, body))
            self.send_header("Content-Length", "0")
            self.end_headers()

        do_GET = do_POST = respond

        def log_message(self, *a):
            pass

    srv = ThreadingHTTPServer(("127.0.0.1", 0), H)
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    return srv, f"http://127.0.0.1:{srv.server_address[1]}"


def by_corpus(target):
    """Servidor que segue o corpus: responde o status da sonda que casa exatamente."""
    def answer(h, body):
        for _, expected, method, tgt, route, headers in check.load_probes():
            if (tgt, method, route) != (target, h.command, h.path):
                continue
            if any(h.headers.get(k) != headers.get(k) for k in KEYS):
                continue
            if "Host" in headers and h.headers.get("Host") != headers["Host"]:
                continue
            if "Host" not in headers and not h.headers.get("Host", "").startswith("127.0.0.1"):
                continue
            return min(expected)
        return 500
    return answer


class FleetProbeTests(unittest.TestCase):
    def run_against(self, local, tailnet):
        urls = []
        for answer in (local, tailnet):
            srv, url = server(answer)
            self.addCleanup(srv.shutdown)
            urls.append(url)
        ctx = Context.discover([], local_url=urls[0], tailnet_url=urls[1])
        return list(check.check(ctx))

    def test_corpus_parses(self):
        probes = list(check.load_probes())
        self.assertTrue(probes)
        self.assertTrue(all(m in ("GET", "POST") and t in ("local", "tailnet") for _, _, m, t, _, _ in probes))

    def test_open_server_fails_every_probe(self):
        # um servidor que aceita tudo (200) diverge de todas as sondas
        open_ = lambda h, b: 200
        self.assertEqual(len(self.run_against(open_, open_)), len(list(check.load_probes())))

    def test_compliant_server_is_clean(self):
        self.assertEqual([d for _, d in self.run_against(by_corpus("local"), by_corpus("tailnet"))], [])

    def test_one_regression_is_reported(self):
        # o servidor esquece o Sec-Fetch-Site em /api/sessions (o caso do binário sem a PR #27)
        good = by_corpus("local")
        regressed = lambda h, b: 401 if h.path == "/api/sessions" and h.headers.get("Sec-Fetch-Site") else good(h, b)
        details = [d for _, d in self.run_against(regressed, by_corpus("tailnet"))]
        self.assertEqual(len(details), 1)
        self.assertIn("[X1] esperava 403, veio 401", details[0])

    def test_service_down(self):
        ctx = Context.discover([], local_url="http://127.0.0.1:9", tailnet_url="http://127.0.0.1:9")
        findings = list(check.check(ctx))
        self.assertEqual(len(findings), 2)  # uma por alvo, sem repetir por sonda
        self.assertTrue(all("não responde" in d for _, d in findings))

    def test_posts_never_carry_json(self):
        bodies = []

        def capture(h, body):
            if h.command == "POST":
                bodies.append(body)
            return 200
        self.run_against(capture, capture)
        self.assertTrue(bodies)
        self.assertEqual(set(bodies), {b"x"})


if __name__ == "__main__":
    unittest.main()
