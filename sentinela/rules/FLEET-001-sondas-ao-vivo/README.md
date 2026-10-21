# FLEET-001: serviço rodando diverge das sondas de ingest e autenticação

**Severidade:** alta · **Tipo:** ao vivo (`--live`) · **Origem:** auditoria de 2026-10-20 (run-1 e run-2), PRs #26 e #27

## Problema

A run-2 da auditoria provou as correções **no código**, com `httptest`. Isso não diz nada sobre o binário que está rodando: ele pode ter sido compilado antes, e o serviço pode ter sido reiniciado com outra flag, sem token, ou numa porta errada.

Esta regra repete os casos da run-2 contra o serviço de verdade, nos dois listeners: `127.0.0.1:7777` (HTTP, por onde o hook entra) e o IP da tailnet (HTTPS).

## Por que importa

O `/event` e o `/statusline` não pedem token. O que protege essas rotas é a recusa de tudo que tem cara de navegador (`Origin`, `Sec-Fetch-Site`, `Content-Type` diferente de `application/json`). Se essa recusa sumir, qualquer site aberto no navegador do dono volta a forjar sessões e alertas de push.

As rotas de leitura dependem do token, e de recusar o pedido cross-site antes de testar o token, para que uma página não esgote as tentativas do dono.

## Famílias de sonda (`probes.txt`)

| Família | O que prova |
|---|---|
| I1 | ingest recusa pedido de navegador: 403 com `Origin`/`Sec-Fetch-Site`, 415 sem `application/json` exato |
| I2 | `/event` vindo da tailnet recebe 403: só o loopback alimenta o painel |
| A1 | leitura sem credencial recebe 401 (ou 421 no modo sem token), inclusive com `Host` de rebinding |
| X1 | subrecurso cross-site recebe 403 antes de testar o token (PR #27) |
| L1 | controle: o pedido igual ao do hook passa do portão (400, porque o corpo não é JSON) |

## Garantias das sondas

- **Nenhuma muda estado.** Todo POST leva o corpo `x`, que não é JSON. Se um portão falhar, o servidor responde 400 em vez de aplicar o evento.
- **Nenhuma leva token.** O throttle só conta token errado (`tryToken`), então as sondas não trancam o dono.
- O certificado HTTPS não é verificado, porque a sonda vai pelo IP e o certificado é do nome `*.ts.net`. Nada secreto é enviado.

## Remediação

Achado em `X1` ou `A1` com o código certo no `main`: o binário está velho, veja o FLEET-002. Recompile, instale e reinicie (`systemctl --user restart claude-fleet`).

Achado em `I1`/`I2` com o binário atual: regressão no código. Abra uma issue com a sonda que falhou.

## Como registrar uma sonda nova

Uma linha em `probes.txt`, na família certa: `<status> <MÉTODO> <local|tailnet> <caminho> [Cabeçalho=valor ...]`. O teste `test_check.py` confere o formato e o comportamento do check com servidores falsos.
