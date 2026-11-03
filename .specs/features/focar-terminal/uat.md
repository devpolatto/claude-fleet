# focar-terminal — UAT

**Data:** 2026-11-02 · **Base:** `feat/focar-terminal` em `0422228` · **Navegador:** desktop (GNOME/Wayland) e celular de acompanhamento, pela tailnet
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
| FOCO-01: sessão → pane pelo `sessions/<pid>.json` | ✅ no serviço real: `tmux 1:0.0`, `tmux 6:1.0`, `tmux 4:0.0` e "fora" nas quatro sessões abertas. Ver a observação 1 |
| FOCO-02: localização no pop-up, em todo aparelho | ✅ desktop e celular |
| FOCO-03: só metadado sai do servidor | ✅ a rota devolve só `estado`, `rotulo` e `podeAbrir`; o log registra só `terminal aberto em tmux 4:0.0` |
| FOCO-04: ícone só com pane vivo e aparelho autorizado | ✅ ícone nas sessões em tmux; sessão do Claude fora do tmux mostrou "fora do tmux" sem ícone |
| FOCO-05: janela nova na frente, no pane certo | ✅ a janela apareceu na frente do navegador, no pane da sessão; `C-b d` fechou a janela e manteve a sessão |
| FOCO-06: regras do pedido (403 para os demais) | ✅ testes `TestAbrirRegrasDoPedido`; aparelho fora da lista não testado à mão |
| FOCO-07: avisos, botão desabilitado, 1 janela a cada 3 s | ✅ no navegador pelo `ui-figma` (todos os códigos, com o POST interceptado) e nos testes `TestAbrirSegundoPedidoEm3sRecebe429`; clique duplo real ➖ |
| FOCO-08: unit enxerga o socket do tmux | ✅ `/proc/<pid>/root/tmp/tmux-1000/default` visível de dentro do serviço |
| FOCO-09: AD-010 e Figma | ⚠️ AD-010 feita; o Figma ficou pendente (MCP sem autenticação) e foi descrito no PR para sincronizar antes do merge |
| FOCO-10: `-focus-from` e log da partida | ✅ `terminal liberado para: a própria máquina e <IP do celular>`; o toque no celular abriu a janela no desktop |
| Quatro botões no topo do pop-up, no celular | ➖ nenhuma sessão aguardando durante o UAT; pela conta (~210 px) cabe |
| `reviewer` | ✅ dois achados (média: `${VAR}` expandido pelo `systemd-run` no título; baixa: limite do título em bytes), corrigidos em `c0dffaf` |
| Gate full (`gofmt`, `vet`, `test -race`, `hook/test.sh`) | ✅ |

## Observações

1. **A primeira instalação mostrou tudo como "desconhecido".** No serviço, o `PrivateTmp` da unit de usuário cria um user namespace, e o kernel nega `readlink /proc/<pid>/fd/0` do processo do Claude. Fora do serviço o mesmo código funcionava. Corrigido em `0422228`, conferindo o tty pelo `tty_nr` do `stat`; registrado como L-009.
2. **Página antiga aberta.** Depois de instalar o binário, o pop-up só mostrou o ícone depois de recarregar a página (o app instalado também precisa ser reaberto). Não há cache no service worker; era a aba carregada antes da troca.
