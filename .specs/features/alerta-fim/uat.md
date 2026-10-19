# alerta-fim — UAT

**Data:** 2026-10-18 · **Base:** `main` em `e37d06f` · **Navegador:** Chromium headless (Playwright), `--autoplay-policy=no-user-gesture-required`
**Ambiente:** servidor em `127.0.0.1:7798` com `-projects` e `-state-dir` vazios; eventos de hook por `POST /event`. Um script na página registra frequência e instante de cada oscilador iniciado. Nenhum dado real.

Legenda: ✅ verificado · ⚠️ desvio

## Roteiro

Antes de carregar a página: P nasce parada (`SessionStart`) e Q entra em ativo (`UserPromptSubmit`). Som salvo como ligado. Tempos contados a partir de 1 s depois da carga.

| t | Evento | Esperado | Tocou |
| --- | --- | --- | --- |
| carga | linha de base (P parado, Q ativo) | nada | nada ✅ |
| 1 s | `SessionStart` N; `UserPromptSubmit` L, C, W, S, M | nada | nada ✅ |
| 2 s | `SubagentStart` S/a1 | nada | nada ✅ |
| 4 s | `Stop` C (turno de 3 s) | nada (FIM-02) | nada ✅ |
| 5 s | `SubagentStop` S/a1 | nada (FIM-03) | nada ✅ |
| 6 s | `PermissionRequest` W | aguardando | 880 → 1320 Hz em 6,26 s ✅ |
| 8 s | `PreToolUse` W (volta a ativo) | nada | nada ✅ |
| 10 s | `PermissionRequest` P | aguardando | 880 → 1320 Hz em 10,25 s ✅ |
| 11 s | `Stop` P (de aguardando) | nada (FIM-04) | nada ✅ |
| 12 s | `SessionEnd` S (de ativo) | nada | nada ✅ |
| 15 s | `Stop` Q (meio de turno na carga: 15 s desde a linha de base) | nada | nada ✅ |
| 22 s | `Stop` L (turno de 21 s) | fim (FIM-01) | 660 → 440 Hz em 22,25 s ✅ |
| 24 s | `Stop` W (turno de 23 s, com aguardando no meio) | fim | 660 → 440 Hz em 24,25 s ✅ |
| 26 s | desliga o toggle | nada | nada ✅ |
| 27 s | `Stop` M (turno de 26 s) | nada (FIM-06) | nada ✅ |

| Critério | Resultado |
| --- | --- |
| FIM-01: ativo → parado após ≥ 20 s toca uma vez em ≤ 2 s | ✅ ~0,25 s depois do evento |
| FIM-02: turno curto não toca | ✅ C |
| FIM-03: `SubagentStop` não toca | ✅ S/a1 |
| FIM-04: parado vindo de outro estado não toca | ✅ N (nasce parada), P (de aguardando); S vai a encerrado sem tocar |
| FIM-05: primeiro snapshot é linha de base | ✅ nada na carga; Q, ativa na carga, conta desde a linha de base |
| FIM-06: toggle desligado silencia | ✅ M |
| FIM-07: som distinto | ✅ 660 → 440 Hz, pico 0,18, contra 880 → 1320 Hz, pico 0,3 |
| Erros no console | ✅ nenhum |
| Mudança visual | nenhuma: mesmo toggle, nada no layout |

A reconexão, que regrava o início dos turnos na nova linha de base, foi conferida só no código: o servidor guarda o estado em memória, então reiniciá-lo para derrubar o SSE apagaria as sessões. A primeira versão mantinha o início antigo, e a revisão mostrou que um turno curto começado durante a queda tocaria. Fora do roteiro: `desconhecido` → parado (boot) não foi simulado, porque exige o Bootstrap; o caminho é o mesmo de P (status anterior diferente de ativo).

## Painel real

Serviço systemd com o binário da branch, painel aberto no navegador com o som ligado, eventos de uma sessão sintética `uat-fim` mandados pelo loopback. O turno curto (5 s) ficou em silêncio; o longo (23 s) tocou as duas notas descendentes no `Stop`. ✅ Confirmado de ouvido pelo dono.
