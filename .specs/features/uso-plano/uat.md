# uso-plano — UAT

**Data:** 2026-10-18 · **Base:** `feat/uso-plano` em `a2890f3` · **Navegador:** celular do usuário, pela tailnet
**Ambiente:** o serviço systemd real da máquina, com o binário da branch e `CLAUDE_FLEET_ARGS` declarando duas contas reais (**pessoal** e **equanimus**). O `statusLine` foi registrado por `CLAUDE_CONFIG_DIR=<dir> claude-fleet install-hooks -yes` nas duas, com backup datado de cada `settings.json`. O `~/.claude`, não declarado no serviço, ficou de fora: o uso dele seria descartado como "outra".

Legenda: ✅ verificado · ⚠️ desvio · ➖ não verificado neste UAT

## Resultado

| Critério | Resultado |
| --- | --- |
| USO-01: script de status line repassa ao loopback, regras do hook | ✅ dados das duas contas chegaram segundos depois do `install-hooks`; o Claude Code seguiu normal; casos 7–11 do `hook/test.sh` |
| USO-02: lista branca só com `rate_limits`, conta pelo `transcript_path` | ✅ `/api/uso` com `pessoal` e `equanimus`, cada uma com o próprio par de janelas; `uso-plano.json` sem caminho, `session_id` nem `cwd` |
| USO-03: `/statusline` só do loopback | ✅ testes (`TestStatuslineSoDoLoopback`) e revisão |
| USO-04: valor novo por SSE e persistido | ✅ `uso-plano.json` em 600; o valor volta depois de reiniciar o serviço |
| USO-05: `install-hooks`/`uninstall-hooks` cuidam do `statusLine` | ✅ só o `statusLine` mudou em cada `settings.json` (hooks iguais), conferido contra o backup |
| USO-06: segunda tela por arrasto, pontinhos e setas | ✅ no celular, em pé e deitado |
| USO-07: medidores com percentual, reset, contagem e "atualizado há" | ✅ números iguais aos do `/usage` de cada conta, conferidos pelo usuário |
| USO-08: tokens de cor, movimento reduzido, vertical e horizontal | ✅ no celular; movimento reduzido conferido no navegador headless (T10) |
| USO-09 (P2): tokens e custo das janelas | ✅ tabelas das duas contas no celular |
| Rodapé do Claude Code sem as dicas | ✅ efeito esperado (Tech Decisions), confirmado nas duas contas |
| `reviewer` | ✅ nenhum achado alto ou médio; os dois baixos de código corrigidos em `ca46626` e `a2890f3`; o de status dos documentos, neste fechamento |
| Gate full (`gofmt`, `vet`, `test -race`, `hook/test.sh` com 11 casos) | ✅ |
| Figma | ✅ tela de uso, componentes e janelas (T12, T15) |

Valores na hora do UAT, pelo painel: pessoal com 15% na sessão (reinicia às 22:30) e 16% na semana; equanimus com 1% e 74%.

## Observações

1. **Mensagens repetidas entre transcripts.** O `reviewer` levantou a dúvida e a contagem nos transcripts reais confirmou: 36 `message.id` por conta aparecem em mais de um arquivo nos últimos 7 dias (sessões retomadas), cerca de 0,5% dos tokens. A soma das janelas passou a deduplicar por conta (`a2890f3`).
2. **Logo depois do reset semanal**, a semana começa depois do início da janela de 5 h; a primeira versão do P2 deixava as mensagens desse intervalo fora da janela de 5 h. Corrigido no mesmo commit.
3. **Desfazer:** `CLAUDE_CONFIG_DIR=<dir> claude-fleet uninstall-hooks` remove hooks e `statusLine` do Fleet; os backups `settings.json.bak-20261018-174031` ficam em cada conta.
