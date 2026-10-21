"""FLEET-001: o claude-fleet rodando responde às sondas como o código promete."""
import http.client
import re
import ssl
import subprocess
from pathlib import Path
from urllib.parse import urlsplit

ID = "FLEET-001"
TITLE = "serviço rodando diverge das sondas de ingest e autenticação"
SEVERITY = "alta"
LIVE = True
FIXTURES = False  # testado em test_check.py, contra servidores falsos

PROBES = Path(__file__).resolve().parent / "probes.txt"
BODY = b"x"  # não é JSON: um portão furado dá 400, nunca aplica evento


def load_probes(path=PROBES):
    family = "?"
    for line in path.read_text().splitlines():
        if m := re.match(r"#\s*\[(\w+)\]", line):
            family = m.group(1)
            continue
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        expected, method, target, route, *headers = line.split()
        yield family, {int(c) for c in expected.split("|")}, method, target, route, \
            dict(h.split("=", 1) for h in headers)


def tailnet_ip():
    try:
        out = subprocess.run(["tailscale", "ip", "-4"], capture_output=True, text=True, timeout=5).stdout
    except (OSError, subprocess.TimeoutExpired):
        return None
    return out.split()[0] if out.split() else None


def connection(url):
    u = urlsplit(url)
    if u.scheme == "https":
        # o certificado é do nome *.ts.net, e a sonda vai pelo IP; nada secreto é enviado
        ctx = ssl.create_default_context()
        ctx.check_hostname = False
        ctx.verify_mode = ssl.CERT_NONE
        return http.client.HTTPSConnection(u.hostname, u.port, timeout=3, context=ctx)
    return http.client.HTTPConnection(u.hostname, u.port, timeout=3)


def send(url, method, route, headers):
    conn = connection(url)
    try:
        conn.request(method, route, body=BODY if method == "POST" else None, headers=headers)
        return conn.getresponse().status
    except OSError as e:
        return e
    finally:
        conn.close()


def check(ctx):
    targets = {"local": ctx.option("local_url", "http://127.0.0.1:7777")}
    if url := ctx.option("tailnet_url"):
        targets["tailnet"] = url
    elif ip := tailnet_ip():
        targets["tailnet"] = f"https://{ip}:{ctx.option('tailnet_port', 7777)}"

    down = set()
    for family, expected, method, target, route, headers in load_probes(ctx.option("probes", PROBES)):
        url = targets.get(target)
        if url is None:
            if target not in down:
                down.add(target)
                yield target, "alvo sem endereço (Tailscale fora?): sondas puladas"
            continue
        if target in down:
            continue
        got = send(url, method, route, headers)
        if isinstance(got, Exception):
            down.add(target)
            yield url, f"serviço não responde ({got.__class__.__name__}): sondas de {target} puladas"
            continue
        if got not in expected:
            want = "|".join(map(str, sorted(expected)))
            hdrs = " ".join(f"{k}={v}" for k, v in headers.items())
            yield url, f"[{family}] esperava {want}, veio {got}: {method} {route} {hdrs}".rstrip()
