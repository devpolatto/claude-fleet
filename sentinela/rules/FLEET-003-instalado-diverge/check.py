"""FLEET-003: hook ou unit instalados diferem dos do repo."""
import filecmp

ID = "FLEET-003"
TITLE = "hook ou unit instalados divergem do repo"
SEVERITY = "média"


def check(ctx):
    repo = ctx.path_option("repo", ".")
    for d in ctx.claude_dirs:
        installed = d / "claude-fleet"
        if not installed.is_dir():
            continue
        for script in sorted(installed.glob("*.sh")):
            source = repo / "hook" / script.name
            if not source.is_file():
                yield script, f"não existe hook/{script.name} no repo: sobra de versão antiga?"
            elif not filecmp.cmp(script, source, shallow=False):
                yield script, f"difere de hook/{script.name}: rode claude-fleet install-hooks"
    if unit := ctx.option("unit"):
        unit = ctx.path_option("unit", unit)
        source = repo / "deploy" / unit.name
        if not unit.is_file():
            yield unit, "unit não instalada"
        elif source.is_file() and not filecmp.cmp(unit, source, shallow=False):
            yield unit, f"difere de deploy/{unit.name}: copie e rode systemctl --user daemon-reload"
