# focar-terminal Specification

> Status: **aprovada** (2026-11-02). Só tmux e Ptyxis.

## Problem Statement

Quando o alerta toca (sessão aguardando permissão ou tarefa concluída), tenho várias sessões do tmux, cada uma numa aba do terminal, e várias janelas abertas. Perco tempo procurando em qual aba e pane está o Claude que precisa de mim. O painel já sabe qual sessão tocou o alerta, mas não diz onde ela roda nem me leva até lá.

Todo terminal abre dentro do tmux (perfil Hyper do Ptyxis, `exec tmux new-session`), então o painel consegue saber o pane de cada sessão. Teste de 2026-11-02: uma janela nova do Ptyxis aberta por um processo em segundo plano (`systemd-run --user`) aparece na frente do navegador no GNOME/Wayland. Por isso o painel abre uma janela nova, já no pane certo, em vez de tentar achar e trazer a aba existente.

## Goals

- [ ] Do alerta ao pane certo em 2 cliques, no desktop ou no celular de acompanhamento: clique no tile (abre o pop-up) e clique no ícone de terminal. Uma janela nova do terminal aparece na frente, já na sessão do tmux e no pane do Claude.
- [ ] Em qualquer aparelho, o pop-up mostra onde a sessão está (ex.: `tmux 6:1.0`), para achar na mão quando não dá para clicar.
- [ ] Nenhuma interação com a sessão do Claude: o painel só abre uma janela de terminal presa àquele pane, nunca digita nada.

## Out of Scope

| Feature | Reason |
| --- | --- |
| Trazer para a frente a aba ou janela já existente | No Wayland o GNOME não deixa sem uma extensão do GNOME Shell; a janela nova resolve o caso |
| Reaproveitar a janela aberta por um clique anterior | Janela existente não vem para a frente (mesmo motivo); cada clique abre uma janela nova, que se fecha com `prefix + d` |
| Sessões fora do tmux | O terminal padrão sempre abre no tmux; fora dele o pop-up diz só "fora do tmux" |
| Outros emuladores além do Ptyxis | É o terminal da máquina; trocar exige só mudar o comando fixo, numa feature futura se precisar |
| `send-keys` ou qualquer tecla enviada ao pane | Viola o "somente leitura" do ponto de vista da sessão do Claude (AD-003) |
| Ir para o terminal de qualquer aparelho com token | Decisão do usuário (2026-11-02): só a própria máquina e os aparelhos listados em `-focus-from`; os demais só veem a localização |
| Menu do botão direito ou duplo clique no tile | Decisão do usuário (2026-11-02): o gatilho é um ícone no pop-up; o tile continua só abrindo o pop-up |
| Servidores tmux com socket próprio (`tmux -L`/`-S`) | Só o servidor padrão do usuário (`/tmp/tmux-<uid>/default`) |

---

## User Stories

### P1: Ver onde a sessão roda ⭐ MVP

**User Story**: Como dono da máquina, quero ver no pop-up em qual pane do tmux cada sessão está, para achar o terminal mesmo quando não posso clicar.

**Why P1**: É a base das outras histórias e já vale sozinha, inclusive no celular.

**Acceptance Criteria**:

1. WHEN abro o pop-up de uma sessão que roda no tmux THEN ele SHALL mostrar a localização no formato `tmux <sessão>:<janela>.<pane>` (os números que o tmux mostra na barra), ex.: `tmux 6:1.0`.
2. WHEN a sessão roda fora do tmux THEN o pop-up SHALL mostrar "fora do tmux".
3. WHEN não dá para saber (arquivo da sessão ausente, PID morto ou reaproveitado, tmux fora do ar) THEN o pop-up SHALL mostrar "local desconhecido", sem erro.
4. WHEN o pane muda de lugar (janela movida, sessão renomeada ou renumerada) THEN a localização SHALL acompanhar, porque o servidor guarda o ID do pane (`%N`) e consulta o tmux na hora.
5. O servidor SHALL expor só a localização (números/nome da sessão do tmux), nunca o conteúdo do pane nem o comando em execução (AD-003).

**Independent Test**: Abrir o pop-up da sessão que roda em `tmux 6:1.0` e ver esse texto; mover a janela no tmux (`move-window`), reabrir e ver o número novo; abrir o pop-up de uma sessão fora do tmux e ver "fora do tmux".

---

### P1: Abrir o terminal da sessão pelo ícone do pop-up ⭐ MVP

**User Story**: Como dono da máquina, quero clicar no tile da sessão que tocou o alerta e, no pop-up, clicar num ícone de terminal, para uma janela de terminal aparecer na frente de tudo já no pane daquela sessão.

**Why P1**: É o pedido.

**Acceptance Criteria**:

1. WHEN abro o pop-up de uma sessão que roda num pane vivo do tmux, num aparelho autorizado (a própria máquina ou um IP de `-focus-from`), THEN o canto superior do pop-up (`.det-topo`, junto de "Ocultar" e "Fechar") SHALL mostrar um botão com ícone de terminal, com rótulo acessível "Abrir terminal (tmux 6:1.0)".
2. WHEN a sessão está fora do tmux, com local desconhecido, ou o painel está aberto num aparelho não autorizado THEN o ícone SHALL não aparecer (o pop-up continua mostrando a localização em texto).
3. WHEN clico no ícone THEN SHALL abrir uma janela nova do Ptyxis, na frente das outras, presa à sessão do tmux do pane (`tmux attach-session`), com a janela e o pane da sessão do Claude selecionados; o título da janela SHALL citar a localização; um aviso curto no pop-up SHALL confirmar ("Terminal aberto: tmux 6:1.0").
4. WHEN a sessão do tmux não está aberta em nenhuma aba THEN o clique SHALL funcionar igual (a janela nova passa a ser o terminal dela).
5. WHEN fecho a janela nova (`prefix + d` ou fechando a janela) THEN a sessão do tmux e o Claude SHALL continuar rodando, e as abas que já mostravam a sessão SHALL continuar como estavam.
6. WHEN abro o pop-up de um subagente (pelo mini-tile) THEN o ícone SHALL levar ao pane da sessão-mãe.
7. WHEN o servidor recusa ou falha THEN o aviso SHALL dizer o motivo ("Sessão encerrada", "Não consegui abrir o terminal") e o pop-up SHALL continuar aberto.
8. WHEN um pedido chega de um aparelho não autorizado, mesmo feito na mão com o token, THEN o servidor SHALL recusar com 403.
9. O clique no tile SHALL continuar só abrindo o pop-up, sem abrir nada.

**Independent Test**: Com o navegador na frente e o alerta tocando na sessão 7, clicar no tile da 7, clicar no ícone de terminal e ver uma janela do Ptyxis surgir na frente do navegador, no pane da 7; fechar com `prefix + d` e ver que a aba original da sessão 7 continua igual.

---

### P1: Liberar o terminal para o celular de acompanhamento ⭐ MVP

**User Story**: Como dono da máquina, quero liberar o ícone de terminal no celular que fica ao lado do desktop só acompanhando os agentes, para abrir o terminal certo sem tirar a mão do celular, sem liberar isso para qualquer aparelho que tenha o token.

**Why P1**: Decisão do usuário (2026-11-02): o celular de acompanhamento é onde o alerta costuma ser visto.

**Acceptance Criteria**:

1. WHEN o serviço sobe com `-focus-from <ip>[,<ip>...]` (IPs do Tailscale, IPv4 ou IPv6) THEN pedidos vindos desses IPs SHALL ser aceitos como os da própria máquina, e o pop-up neles SHALL mostrar o ícone.
2. WHEN `-focus-from` não é passado THEN SHALL valer só a própria máquina (loopback ou IP de uma interface local).
3. WHEN `-focus-from` traz algo que não é IP (nome, faixa CIDR, vazio entre vírgulas) THEN o serviço SHALL recusar subir, com erro claro, em vez de ignorar a entrada.
4. WHEN um pedido chega de um IP fora da lista e de fora da máquina, mesmo com o token certo THEN o servidor SHALL responder 403 e o pop-up dele SHALL não mostrar o ícone.
5. WHEN o serviço sobe com `-focus-from` THEN o log de início SHALL listar os IPs autorizados, para o dono conferir.

**Independent Test**: Subir o serviço com `-focus-from` igual ao IP do Tailscale do celular de acompanhamento; tocar no ícone pelo celular e ver a janela surgir no desktop; abrir o painel num segundo aparelho fora da lista e confirmar que o ícone não aparece e que um `curl` com o token recebe 403; subir com `-focus-from celular` e ver o serviço recusar.

---

## Edge Cases

- WHEN a sessão do Claude termina entre o pop-up abrir e o clique no ícone THEN o pedido SHALL falhar com "Sessão encerrada" e nenhuma janela abrir (o pane pode ter outro programa agora).
- WHEN clico várias vezes seguidas no ícone (ou toco duas vezes no celular) THEN SHALL abrir uma janela só: o botão fica desabilitado enquanto o pedido corre e o servidor ignora um segundo pedido da mesma sessão em menos de 3 s.
- WHEN o Ptyxis ou o `systemd-run` não estão instalados THEN o pedido SHALL falhar com "Não consegui abrir o terminal", e o log do serviço SHALL dizer qual comando faltou.
- WHEN não há sessão gráfica (servidor rodando sem GNOME logado) THEN o pedido SHALL falhar do mesmo jeito, sem travar o servidor (o comando tem prazo).
- WHEN a janela nova e uma aba antiga mostram a mesma sessão do tmux THEN trocar de janela do tmux numa SHALL trocar na outra (comportamento padrão do tmux), aceito.
- WHEN o serviço do claude-fleet é reiniciado THEN as janelas abertas por ele SHALL continuar abertas (o terminal roda numa unit própria, fora do serviço).
- WHEN o PID do arquivo da sessão foi reaproveitado por outro processo THEN o `procStart` não bate e a sessão SHALL ficar com local desconhecido.
- WHEN o tmux não está instalado ou o servidor dele não está rodando THEN tudo SHALL mostrar "local desconhecido" e nenhum log de erro por sessão a cada ciclo.

---

## Restrições de projeto (vêm do STATE e do ambiente)

- **De onde vem o pane.** O Claude Code grava `<CLAUDE_CONFIG_DIR>/sessions/<pid>.json` com `sessionId`, `procStart` e `tmux` (`"<sessão>:@<janela>.%<pane>"`). O servidor lê só esses três campos do arquivo da conta da sessão (AD-009) e confere `procStart` com o campo 22 do `/proc/<pid>/stat`. É formato interno do Claude Code e pode mudar: sem ele, cai em "local desconhecido". Hook e `ingest` não mudam.
- **Só stdlib, sem shell, comandos fixos.** Dois comandos, por `os/exec` com argumentos fixos: `tmux display-message -p -t %N <formato>` para localizar e `systemd-run --user --collect ptyxis --new-window -T <título> -- tmux attach-session -t %N` para abrir. O único dado variável é o `%N` validado e o rótulo montado a partir da saída do próprio tmux.
- **Quem pode pedir.** A rota de ação exige o token/cookie de sempre e, além disso, peer que seja a própria máquina (loopback ou um endereço das interfaces locais, já que o serviço escuta no IP do Tailscale) ou um IP listado em `-focus-from`. O IP de origem identifica o aparelho porque o Tailscale autentica cada nó; um celular perdido se bloqueia tirando-o da tailnet. Valem também os cuidados da L-005/L-006 contra pedido vindo de outra página: `POST` com `Content-Type: application/json` e `Sec-Fetch-Site: same-origin`.
- **Serviço systemd.** A unit tem `PrivateTmp=yes`, mas o teste de 2026-11-02 mostrou que ele não vale nesta unit de usuário: o serviço já enxerga `/tmp/tmux-<uid>/`. A unit SHALL ganhar `BindPaths=-/tmp/tmux-%U` para continuar funcionando onde o `PrivateTmp` valer (ver design).
- **AD nova.** É a primeira ação do painel sobre a máquina: abrir uma janela de terminal presa a um pane do tmux. Precisa de uma AD no `STATE.md` que delimite: comando fixo, nunca envia tecla, só da própria máquina e dos aparelhos de `-focus-from`.
- **Figma.** O ícone de terminal no topo do pop-up, o aviso e a linha de localização são mudança visual: o agente `ui-figma` atualiza o protótipo no mesmo PR.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| FOCO-01 | P1: Ver — resolver sessão → pane pelo `sessions/<pid>.json`, com `procStart`, tty e ID `%N` | Design | Pending |
| FOCO-02 | P1: Ver — localização `tmux S:J.P`, "fora do tmux" ou "local desconhecido" no pop-up, em todo aparelho | Design | Pending |
| FOCO-03 | P1: Ver — só metadado de localização sai do servidor (AD-003) | Design | Pending |
| FOCO-04 | P1: Abrir — ícone de terminal no topo do pop-up (sessão e subagente), só com pane vivo e aparelho autorizado | Design | Pending |
| FOCO-05 | P1: Abrir — janela nova do Ptyxis por `systemd-run --user`, `attach-session` no pane, título com a localização | Design | Pending |
| FOCO-06 | P1: Abrir — rota de ação: token + aparelho autorizado + JSON + same-origin; 403 dos demais | Design | Pending |
| FOCO-07 | P1: Abrir — avisos de resultado e de falha, botão desabilitado durante o pedido, 1 janela por sessão a cada 3 s | Design | Pending |
| FOCO-08 | Restrição — unit systemd enxerga o socket do tmux | Design | Pending |
| FOCO-09 | Restrição — AD nova no `STATE.md` e Figma sincronizado | Design | Pending |
| FOCO-10 | P1: Celular — flag `-focus-from` com IPs, validada na subida e listada no log; sem ela, só a própria máquina | Design | Pending |

**Coverage:** 10 total, 0 mapeados para tasks, 10 sem mapeamento (aguardando design).

---

## Success Criteria

- [ ] Ao ouvir o alerta, chego ao pane da sessão em 2 cliques (tile → ícone) e ≤ 5 s, com a janela aparecendo na frente do navegador.
- [ ] Pelo celular de acompanhamento (em `-focus-from`), o ícone abre a janela no desktop em ≤ 2 s.
- [ ] Num aparelho fora da lista, o pop-up diz em qual `tmux S:J.P` está a sessão, sem o ícone, e nenhum pedido vindo dele é aceito.
- [ ] Nenhum comando além dos dois fixos é executado pelo servidor (conferido no teste e na revisão).
