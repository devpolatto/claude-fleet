# fleet-mvp — UAT (T18)

**Data:** 2026-10-15 · **Claude Code:** 2.1.301 · **Binário:** `~/.local/bin/claude-fleet` (commit `10518e3`)
**Ambiente:** hooks reais instalados em `~/.claude/settings.json` (19:46:57; backup `settings.json.bak-20261015-194657`), servidor em `127.0.0.1:7777` sem token.
**Escopo desta rodada:** parte local. A parte do celular depende do Tailscale (B-001).

Legenda: ✅ verificado · 👀 falta conferir no navegador · 📱 falta conferir no celular · ⚠️ desvio

## Success Criteria

| Critério | Resultado | Evidência |
| --- | --- | --- |
| 2 terminais + 1 subagente em background no celular, transições ≤ 2 s | ✅ latência local · 📱 celular | Hook → snapshot no `/stream`: 257 ms em UserPromptSubmit, PermissionRequest, PostToolBatch e Stop (a janela de coalescência é de 250 ms). Subagente `Explore` real em segundo plano: apareceu ativo com `LastTool=Bash` e passou a concluído. Segundo terminal: 👀 |
| Tokens do modal = soma independente do transcript | ✅ | T14: sessão do spike + 1 subagente, diferença 0 nas 5 categorias (script Python independente do Go) |
| Servidor parado não atrasa o Claude Code > 1 s | ✅ | Hook real com nada escutando na porta: 9–11 ms, exit 0. Com servidor mudo, o `-m 1` corta em ~1,0 s (`hook/test.sh`). Com servidor no ar: 9 ms |
| Nenhum trecho de prompt de teste no log, API ou página | ✅ | Texto-sentinela enviado em `prompt`, `tool_input` e `last_assistant_message` pelo hook real: 0 ocorrências no log do servidor, em `/api/sessions`, `/api/session/{id}`, `/` e `/stream` |

## Critérios P1

### Grade de agentes ao vivo

| # | Critério | Resultado |
| --- | --- | --- |
| 1 | Tile novo no `SessionStart` com projeto e id curto | ✅ API (`Project=uat`, `ShortID` de 8 caracteres) · 👀 visual |
| 2 | Ativo com pulso e última ferramenta | ✅ API (`ativo`, `LastTool=Bash` nesta sessão) · 👀 pulso |
| 3 | Aguardando em amarelo; sai com o próximo evento, inclusive `PostToolBatch` | ✅ API (medido acima) · 👀 cor |
| 4 | Parado em cinza no `Stop` | ✅ API · 👀 cor |
| 5 | Encerrado esmaecido no `SessionEnd`; some em 15 min | ✅ API (`encerrado` com `EndedAt`) · ✅ remoção nos testes do T8 · 👀 visual |
| 6 | Sessão criada a partir de evento desconhecido | ✅ (`uat-sentinela-0001` nasceu de `UserPromptSubmit`) |
| 7 | Mudanças sem recarregar, ≤ 2 s | ✅ 257 ms no `/stream` · 👀 na página |

### Subagentes

| # | Critério | Resultado |
| --- | --- | --- |
| 1 | Mini-tile ativo no `SubagentStart`, com o tipo | ✅ API (subagente real `Explore`) · 👀 visual |
| 2 | Atividade atribuída ao subagente | ✅ API (`LastTool` no subagente) |
| 3 | Concluído no `SubagentStop` | ✅ API (`concluido`, `EndedAt` preenchido) |
| 4 | Sessão-pai parada com subagente ativo → mini-tile pulsando | 👀 |
| 5 | `SubagentStop` sem `SubagentStart` ignorado | ✅ testes do T7 (sequência real de compactação do spike) |
| 6 | Pre/PostToolUse da ferramenta `Agent` não mudam o subagente | ✅ testes do T7 |

### Pop-up de detalhes

| # | Critério | Resultado |
| --- | --- | --- |
| 1 | Abre sem navegar; fecha com X, fora e Esc | 👀 (fora do modal só no desktop: no celular o modal ocupa a tela) |
| 2–5 | Tempo de vida ao vivo, tokens por categoria e agente, custo rotulado, lista de subagentes | ✅ capturas do T14 · 👀 contadores avançando |
| 6 | Mini-tile abre o modal focado no subagente | ✅ captura do T14 · 👀 |
| DET-06 | Atualiza enquanto aberto | 👀 (tokens desta sessão subindo em ≤ 5 s) |

### Acesso seguro

| # | Critério | Resultado |
| --- | --- | --- |
| 1 | Sem flags, só `127.0.0.1` | ✅ log: `escutando em http://127.0.0.1:7777` |
| 2 | Bind exposto sem token recusa subir | ✅ código 2 com mensagem (T11) |
| 3 | Token por `?t=` → cookie `HttpOnly`; sem token → 401 | ✅ testes do T11 · 📱 no celular |
| 4 | `/event` fora do loopback → 403 | ✅ testes do T11 · 📱 |
| 5 | Só metadados; corpo nunca logado | ✅ busca do texto-sentinela acima |

### P2

| Critério | Resultado |
| --- | --- |
| `install-hooks` com backup, diff e confirmação | ✅ instalação real: backup datado criado; fora de `hooks` tudo igual (chaves e ordem); os 4 grupos antigos intactos; segunda execução "nada a fazer" |
| `uninstall-hooks` remove só o claude-fleet | ✅ testes do T15 e execução numa cópia da fixture |
| Descoberta no boot (30 min), desconhecido até o primeiro evento | ✅ 6 sessões descobertas no boot; esta passou para ativo no primeiro evento |

## Desvios e observações

- **A instalação dos hooks no settings real foi feita pelo usuário** às 19:46:57, num terminal próprio. O resultado foi conferido contra o backup e está correto.
- ⚠️ **O `settings.json` mudou às 19:21:05** sem relação com o claude-fleet: o bloco `hooks` estava igual ao do T1, sem backup. Provavelmente o próprio Claude Code (modelo, permissões ou modo).
- O primeiro teste de `Stop` falhou por um erro no comando de teste (`printf` com dois argumentos e um `%s` gerou JSON inválido → 400). Refeito, o resultado foi `parado` e depois `encerrado`.

## Conferência no navegador (2026-10-15)

O usuário abriu o painel em <http://127.0.0.1:7777> e confirmou: "está tudo funcionando". Isso cobre de forma geral os itens 👀 acima (tiles, cores por status e modal).

**Reconexão (UI-03):** ✅ O servidor foi encerrado com SIGINT (saída 0, encerramento limpo), ficou 15 s fora e voltou às 19:54:46. O usuário confirmou que "reconectando…" apareceu durante a queda e sumiu sozinho na volta, sem recarregar a página. Na volta, o Store começou vazio (o estado é só em memória): a descoberta no boot trouxe as 2 sessões com transcript recente, em desconhecido, e esta sessão voltou a ativo no evento seguinte.

**Aguardando permissão (segundo terminal):** feito com um `claude` interativo dentro de um `tmux` (modelo haiku, só o hook do claude-fleet, para não disparar o `telegram-notify`).

- ✅ O tile novo apareceu como parado e foi a **aguardando** (amarelo, contador "1 aguardando") enquanto a pergunta de permissão estava na tela. O usuário confirmou o amarelo no navegador.
- ⚠️ **Bug encontrado:** ao responder "No", a sessão voltou ao prompt, mas o tile ficou preso em aguardando. Um registrador de eventos (só nomes) mostrou que nenhum hook chega depois do `PermissionRequest` nesse caso.
- ✅ **Corrigido:** o Refresher passou a usar a linha `system/turn_duration` do transcript (fim de turno) como fallback. Repetido o teste: aguardando → **parado** em 5 s depois do "No".
- ✅ **Sem regressão:** aprovando ("Yes"), aguardando → ativo na hora (`PostToolUse`) → parado em 1 s (`Stop`). Com `/exit`, a sessão foi a **encerrado**.
- Observação: sessões que terminaram enquanto o servidor estava parado voltam no boot como "desconhecido" e só somem pela regra de 12 h sem eventos.

## Pendente

- 👀 `prefers-reduced-motion` (opcional).
- 📱 **Celular:** instalar o Tailscale (B-001), subir com `-addr <IP da tailnet>:7777` e token e repetir a grade e o modal.
