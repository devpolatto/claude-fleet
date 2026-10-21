"""FLEET-004: arquivo com segredo do claude-fleet legível por outro usuário."""
import re
import stat

ID = "FLEET-004"
TITLE = "segredo do claude-fleet com permissão aberta"
SEVERITY = "média"
FIXTURES = False  # o git não guarda 0600; testado em test_check.py

# Só o certificado é público. O resto guarda token, chave privada ou assinaturas.
PUBLIC = {"tls.crt"}
ENV_COPY = re.compile(r"env\.(bak|orig|old).*|env~")


def check(ctx):
    cfg = ctx.path_option("config_dir", "~/.config/claude-fleet")
    if not cfg.is_dir():
        return
    mode = stat.S_IMODE(cfg.stat().st_mode)
    if mode & 0o022:
        yield cfg, f"pasta {oct(mode)}: outro usuário pode trocar os arquivos"
    for f in sorted(cfg.iterdir()):
        if not f.is_file() or f.name in PUBLIC:
            continue
        mode = stat.S_IMODE(f.stat().st_mode)
        if mode & 0o077:
            yield f, f"{oct(mode)}: devia ser 0o600"
        if ENV_COPY.fullmatch(f.name):
            yield f, "cópia do env (tem o token): apague depois de conferir"
