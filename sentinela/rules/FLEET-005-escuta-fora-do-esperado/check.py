"""FLEET-005: o processo claude-fleet escuta fora do loopback e da tailnet."""
import ipaddress
import re
import subprocess
from pathlib import Path

ID = "FLEET-005"
TITLE = "claude-fleet escutando fora do loopback e da tailnet"
SEVERITY = "alta"
FIXTURES = False  # testado em test_check.py, com saída de ss injetada

# Faixas do Tailscale: CGNAT 100.64.0.0/10 e o ULA fd7a:115c:a1e0::/48.
TAILNET = [ipaddress.ip_network("100.64.0.0/10"), ipaddress.ip_network("fd7a:115c:a1e0::/48")]
PROCESS = re.compile(r'users:\(\("claude-fleet"')


def listeners(ctx):
    if path := ctx.option("ss_output"):
        return Path(path).read_text()
    return subprocess.run(["ss", "-ltnpH"], capture_output=True, text=True, timeout=10).stdout


def address(local):
    """'100.64.0.10:7777', '[::]:7777', '127.0.0.1%lo:7777' → endereço sem porta e zona."""
    host = local.rsplit(":", 1)[0].strip("[]").split("%", 1)[0]
    return "0.0.0.0" if host == "*" else host


def allowed(host):
    try:
        ip = ipaddress.ip_address(host)
    except ValueError:
        return False
    ip = getattr(ip, "ipv4_mapped", None) or ip
    return ip.is_loopback or any(ip in net for net in TAILNET)


def check(ctx):
    found = False
    for line in listeners(ctx).splitlines():
        if not PROCESS.search(line):
            continue
        found = True
        local = line.split()[3]
        if not allowed(address(local)):
            yield local, "escuta em endereço fora do loopback e da tailnet: o painel fica exposto na rede"
    if not found:
        yield "claude-fleet", "processo não encontrado entre os listeners (serviço parado?)"
