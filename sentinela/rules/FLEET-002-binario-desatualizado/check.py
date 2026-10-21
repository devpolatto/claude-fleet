"""FLEET-002: o binário implantado não foi compilado das fontes do main."""
import re
import subprocess

ID = "FLEET-002"
TITLE = "binário implantado diverge das fontes do main"
SEVERITY = "alta"
FIXTURES = False  # testado em test_check.py, com um repo git temporário

# O que entra no binário: o hook é embutido (hook/embed.go) e o static também.
SOURCES = ["cmd", "internal", "hook", "go.mod", "go.sum"]


def stamp(binary):
    """vcs.revision e vcs.modified gravados pelo go build, lidos dos bytes do binário."""
    data = binary.read_bytes()
    rev = re.search(rb"vcs\.revision=([0-9a-f]{40})", data)
    mod = re.search(rb"vcs\.modified=(true|false)", data)
    return (rev.group(1).decode() if rev else None), (mod.group(1) == b"true" if mod else None)


def git(repo, *args):
    return subprocess.run(["git", "-C", str(repo), *args], capture_output=True, text=True, timeout=30)


def check(ctx):
    binary = ctx.path_option("binary", "~/.local/bin/claude-fleet")
    repo = ctx.path_option("repo", ".")
    ref = ctx.option("ref", "origin/main")
    if not binary.is_file():
        yield binary, "binário não encontrado"
        return
    rev, modified = stamp(binary)
    if rev is None:
        yield binary, "sem vcs.revision: compilado fora do git ou com -buildvcs=false"
        return
    if modified:
        yield binary, f"compilado de {rev[:7]} com alterações não commitadas (vcs.modified=true)"
    if git(repo, "cat-file", "-e", f"{rev}^{{commit}}").returncode != 0:
        yield binary, f"commit {rev[:7]} não existe no repo: rode git fetch ou recompile"
        return
    diff = git(repo, "diff", "--stat", rev, ref, "--", *SOURCES)
    if diff.returncode != 0:
        yield binary, f"não deu para comparar com {ref}: {diff.stderr.strip()}"
    elif diff.stdout.strip():
        summary = diff.stdout.strip().splitlines()[-1].strip()
        head = git(repo, "rev-parse", "--short", ref).stdout.strip()
        yield binary, f"compilado de {rev[:7]}; {ref} ({head}) mudou as fontes: {summary}"
