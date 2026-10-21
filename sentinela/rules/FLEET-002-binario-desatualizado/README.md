# FLEET-002: binário implantado diverge das fontes do main

**Severidade:** alta · **Achado em:** 2026-10-21, `~/.local/bin/claude-fleet`

## Problema

O binário em produção foi compilado do `9dec6d3` (PR #26). As correções da PR #27 (`e3ed017`) estavam mergeadas no `main`, mas nunca foram implantadas: o cross-site antes do token, os timeouts do listener da tailnet, o redirect do push, os limites de campo e de sessões, e a recusa de FIFO no `transcript_path`.

A auditoria marcou tudo como resolvido, e estava certa sobre o código. O serviço rodando é que continuava sem a correção.

Resolvido no mesmo dia: o binário foi recompilado do `e3ed017`, e o FLEET-001 confirmou as sondas da PR #27 no serviço rodando.

## Por que importa

Correção mergeada não é correção implantada. Sem esta regra, o relatório de segurança e o serviço divergem em silêncio.

## Remediação

Recompile do `main` e reinicie:

```sh
git switch main && git pull
go build -o ~/.local/bin/claude-fleet ./cmd/claude-fleet
systemctl --user restart claude-fleet
```

Depois rode `audit --live` para confirmar com o FLEET-001.

## O que o check verifica

- Lê `vcs.revision` e `vcs.modified` direto dos bytes do binário: o `go build` grava em texto puro, e o `go` nem precisa estar instalado.
- Compara esse commit com `origin/main` (option `ref`) só nas fontes que entram no binário: `cmd/`, `internal/`, `hook/`, `go.mod`, `go.sum`. Commit só de docs não acusa.
- Acusa também binário compilado com alteração não commitada (`vcs.modified=true`) ou de commit desconhecido.

O `origin/main` é o do último `git fetch`. O check não acessa a rede.
