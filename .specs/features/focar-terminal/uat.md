# focar-terminal — UAT

**Data:** 2026-11-02 · **Base:** `feat/focar-terminal` · **Navegador:** desktop (GNOME/Wayland) e celular de acompanhamento, pela tailnet
**Ambiente:** o serviço systemd real da máquina, com o binário e a unit da branch, `CLAUDE_FLEET_ARGS` com `-focus-from <IP do celular>`, Ptyxis 50 e tmux 3.6.

Legenda: ✅ verificado · ⚠️ desvio · ➖ não verificado neste UAT · ⏳ pendente

## Roteiro

1. **Desktop, sessão escondida.** Com uma sessão do Claude num pane do tmux e o navegador na frente de tudo (a aba do terminal escondida ou em outra janela), abrir o pop-up da sessão: a linha de cima mostra `tmux S:J.P` e o ícone de terminal. Clicar: em até 5 s, uma janela nova do Ptyxis aparece **na frente do navegador**, no pane da sessão, com o título `claude-fleet · tmux S:J.P`; o pop-up mostra "Terminal aberto: …".
2. **Fechar sem matar.** Na janela nova, `prefix + d`: a janela fecha, a sessão do tmux e o Claude continuam (a aba original segue funcionando).
3. **Clique duplo.** Dois cliques em menos de 3 s: uma janela só; o segundo mostra "Terminal aberto há pouco".
4. **Celular listado.** No celular do `-focus-from`, o mesmo pop-up mostra o ícone; o toque abre a janela no desktop em até 2 s.
5. **Aparelho não listado.** Num aparelho com token e fora da lista (ou o celular depois de tirá-lo da lista e reiniciar o serviço): a localização aparece, o ícone não.
6. **Fora do tmux.** Sessão aberta num terminal sem tmux: "fora do tmux", sem ícone.
7. **Sessão encerrada.** Encerrar o Claude com o pop-up aberto e clicar: "Sessão encerrada", nenhuma janela.
8. **Subagente.** Pop-up aberto pelo mini-tile de subagente leva à sessão-mãe.
9. **Reinício do serviço.** Com a janela aberta, `systemctl --user restart claude-fleet`: a janela continua aberta.
10. **Log.** A partida registra `terminal liberado para: …`; cada abertura registra só o rótulo (`tmux S:J.P`), sem conteúdo.

## Resultado

| Critério | Resultado |
| --- | --- |
| FOCO-01: sessão → pane pelo `sessions/<pid>.json` | ⏳ (conferido antes do UAT com o `Locate` contra as sessões reais da máquina: `tmux`, `fora`) |
| FOCO-02: localização no pop-up, em todo aparelho | ⏳ |
| FOCO-03: só metadado sai do servidor | ⏳ |
| FOCO-04: ícone só com pane vivo e aparelho autorizado | ⏳ |
| FOCO-05: janela nova na frente, no pane certo | ⏳ |
| FOCO-06: regras do pedido (403 para os demais) | ⏳ (testes `TestAbrirRegrasDoPedido`) |
| FOCO-07: avisos, botão desabilitado, 1 janela a cada 3 s | ⏳ |
| FOCO-08: unit enxerga o socket do tmux | ⏳ |
| FOCO-09: AD-010 e Figma | ⏳ |
| FOCO-10: `-focus-from` e log da partida | ⏳ |
| `reviewer` | ⏳ |
| Gate full | ⏳ |

## Observações

_Preencher no UAT._
