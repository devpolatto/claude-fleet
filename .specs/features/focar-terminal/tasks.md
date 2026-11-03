# focar-terminal Tasks

**Design**: `.specs/features/focar-terminal/design.md`
**Status**: In Progress (2026-11-02)

**Gates** (comandos do `CLAUDE.md`; não há `.specs/codebase/TESTING.md`):

| Gate | Comando |
| --- | --- |
| quick | `go test -race -count=1 ./...` |
| full | `gofmt -l .` vazio, `go vet ./...`, `go test -race -count=1 ./...`, `bash hook/test.sh` |
| browser | Painel no desktop e em largura de celular (vertical e horizontal), pop-up de sessão em tmux, fora do tmux e de subagente |
| real | Serviço instalado (`go build -o ~/.local/bin/claude-fleet ./cmd/claude-fleet` + restart), clique no ícone pelo desktop e pelo celular listado em `-focus-from` |

Os testes de Go rodam em paralelo com segurança (`t.TempDir()`, `Runner` falso, `httptest`). O `hook/test.sh` sobe um servidor real numa porta, então roda sozinho. Nenhum teste automatizado chama tmux, systemd ou Ptyxis de verdade.

---

## Execution Plan

### Phase 1: Fundação (Parallel OK)

```
T1 [P]   account.ConfigDir exportado
T2 [P]   store.TranscriptPath(id)
T3 [P]   terminal: Runner com lista fechada
T4 [P]   verificação manual do attach-session
T5 [P]   unit com BindPaths
T8 [P]   web: SetTerminal, trustedPeer, focusAllowed
```

### Phase 2: Pacote `internal/terminal` (Sequential)

```
T3 ──→ T6 ──→ T7
T4 ─────────→ T7
```

### Phase 3: Rotas (Sequential)

```
T1, T2, T8 ──→ T9 ──→ T10
```

### Phase 4: Ligação e interface (Parallel OK)

```
T7, T10 ──→ T11 [P]   flag -focus-from e main
T10 ──────→ T12 [P]   frontend + Figma (ui-figma)
```

### Phase 5: Fechamento (Sequential)

```
T11, T12 ──→ T13 ──→ T14
T5 ─────────────────→ T14
```

---

## Task Breakdown

### T1: Exportar `account.ConfigDir` [P]

**What**: Renomear o `configDir` privado para `ConfigDir(transcriptPath string) string`, exportado, sem mudar o comportamento; `Resolve` passa a usá-lo.
**Where**: `internal/account/account.go`, `internal/account/account_test.go`
**Depends on**: None
**Reuses**: a função atual (`internal/account/account.go:144`)
**Requirement**: FOCO-01

**Done when**:

- [x] `ConfigDir("<dir>/projects/<proj>/<id>.jsonl")` devolve `<dir>`; caminho fora do formato devolve `""`
- [x] Testes existentes do pacote passam sem alteração
- [x] Gate passa: `go test -race -count=1 ./internal/account/`, com 1 teste novo (`TestConfigDirDeduzDiretorioDaConta`, em tabela)

**Feito** (01a10a3).

**Tests**: unit · **Gate**: quick
**Commit**: `refactor(account): exporta ConfigDir para achar o diretório da conta`

---

### T2: Getter `Store.TranscriptPath` [P]

**What**: `func (s *Store) TranscriptPath(id string) (string, bool)`, sob o mutex de leitura, sem expor a `Session` inteira.
**Where**: `internal/store/store.go`, `internal/store/store_test.go`
**Depends on**: None
**Reuses**: padrão dos getters existentes do `Store`
**Requirement**: FOCO-01

**Done when**:

- [ ] Sessão conhecida devolve o caminho e `true`; desconhecida, `""` e `false`
- [ ] Gate passa: `go test -race -count=1 ./internal/store/`, com 1 teste novo (`TestTranscriptPathDaSessao`)

**Cancelada**: o `Store.Get(id)` já devolve a `Session` com `TranscriptPath` (fora do JSON por `json:"-"`); a rota usa ele, sem getter novo.

**Tests**: unit · **Gate**: quick

---

### T3: `Runner` com lista fechada [P]

**What**: Interface `Runner` (`Run(ctx, name string, args ...string) (string, error)`) e o `execRunner` real, que só executa `tmux display-message …` e `systemd-run --user --quiet --collect --expand-environment=no ptyxis --new-window -T <título> -- tmux attach-session -t %N …` na forma exata do design; qualquer outra chamada devolve erro sem executar.
**Where**: `internal/terminal/exec.go`, `internal/terminal/exec_test.go`
**Depends on**: None
**Reuses**: nada (pacote novo, folha, só stdlib)
**Requirement**: FOCO-05, FOCO-09 (AD-010: só dois comandos fixos)

**Done when**:

- [x] `tmux kill-server`, `tmux send-keys`, `sh -c …`, `systemd-run` com outro programa e `%N` fora de `^%\d+$` são recusados antes do `exec`
- [x] Prazo por `context` (5 s) respeitado; stdout limitado (sem ler saída ilimitada)
- [x] Gate passa: `go test -race -count=1 ./internal/terminal/`, com `TestRunnerRecusaComandoForaDaLista` (tabela com ≥ 5 casos)

**Feito** (3c58667).

**Tests**: unit · **Gate**: quick
**Commit**: `feat(terminal): runner com lista fechada de comandos`

---

### T4: Verificar o `attach-session -t %N` [P]

**What**: Teste manual numa sessão tmux descartável de duas janelas e dois panes: `systemd-run --user --collect ptyxis --new-window -- tmux attach-session -t %N` sozinho já cai no pane certo? Registrar a resposta aqui, para o T7 saber se mantém `select-window`/`select-pane`.
**Where**: este arquivo (seção **Resultado** do T4)
**Depends on**: None
**Requirement**: FOCO-05

**Done when**:

- [x] Resultado anotado (cai ou não no pane `%N` quando a sessão está noutra janela/pane)
- [x] Sessão de teste removida e nenhuma unit `run-*` sobrando

**Resultado** (2026-11-02, tmux 3.6, sessão de 2 janelas × 2 panes, cliente ativo na janela 1 pane 1): `attach-session -t %N` sozinho deixou ativos a janela e o pane do `%N` (`0.0 %80 wa=1 pa=1`). Os `select-window`/`select-pane` saem do comando.

**Tests**: none (verificação manual de comportamento do tmux) · **Gate**: none
**Commit**: junto do T7

---

### T5: Unit systemd com `BindPaths` [P]

**What**: `BindPaths=-/tmp/tmux-%U` na unit, com comentário curto do motivo (L-009).
**Where**: `deploy/claude-fleet.service`
**Depends on**: None
**Requirement**: FOCO-08

**Done when**:

- [x] `systemd-analyze --user verify deploy/claude-fleet.service` sem erro novo
- [ ] Depois de copiar a unit e `systemctl --user daemon-reload && systemctl --user restart claude-fleet`, o serviço sobe e `/tmp/tmux-1000/default` é visível de dentro dele (`nsenter`/`/proc/<pid>/root` ou `ls` via `systemd-run -p` equivalente)

**Feito** (8f8dcd3); a conferência com o serviço reiniciado fica para o T14.

**Tests**: none (arquivo de unit) · **Gate**: real
**Commit**: `fix(deploy): serviço enxerga o socket do tmux mesmo com PrivateTmp`

---

### T6: `Locate`

**What**: `Locate(configDir, sessionID string) Location` como no design: varre `<configDir>/sessions/*.json` (≤ 64 KiB), confere `procStart` com o campo 22 de `<proc>/<pid>/stat`, valida o campo `tmux`, roda `display-message` pelo `Runner` e confere o `pane_tty` contra o `tty_nr` do PID. Raiz de `/proc` injetável.
**Where**: `internal/terminal/terminal.go`, `internal/terminal/terminal_test.go`, `internal/terminal/testdata/sessions/`
**Depends on**: T3
**Reuses**: decodificação parcial de JSON (só `sessionId`, `procStart`, `tmux`, `pid`)
**Requirement**: FOCO-01, FOCO-02, FOCO-03

**Done when**:

- [x] Estados `tmux` (com rótulo `tmux S:J.P`), `fora` (campo `tmux` ausente/nulo) e `desconhecido` (sem arquivo, PID morto, `procStart` diferente, tty diferente, tmux fora do ar, `tmux` malformado)
- [x] Rótulo limpo de caracteres de controle; `Pane` nunca sai na API
- [x] Fixtures anonimizadas (sem caminho pessoal nem nome de máquina)
- [x] Gate passa: `go test -race -count=1 ./internal/terminal/`, com ≥ 7 testes (`TestLocateAchaPaneDaSessao`, `…ProcStartDiferenteDaDesconhecido`, `…SemCampoTmuxEhFora`, `…TtyNaoBateDaDesconhecido`, `…ArquivoGrandeIgnorado`, `…TmuxForaDoArDaDesconhecido`, `…CampoTmuxMalformadoDaDesconhecido`)

**Feito** (3c58667). Desvio: o tty é conferido pelo alvo de `/proc/<pid>/fd/0` (e não pelo `tty_nr`), e `Locate`/`Open` recebem `context.Context`. O `procStart` vem como string no JSON real.

**Tests**: unit · **Gate**: quick
**Commit**: `feat(terminal): localiza o pane do tmux de uma sessão`

---

### T7: `Open`

**What**: `Open(loc Location) error`, montando o `systemd-run` fixo com título `claude-fleet · <rótulo>`; mantém ou tira `select-window`/`select-pane` conforme o resultado do T4.
**Where**: `internal/terminal/terminal.go`, `internal/terminal/terminal_test.go`
**Depends on**: T3, T4
**Requirement**: FOCO-05

**Done when**:

- [x] `Open` de `Location` fora do estado `tmux` devolve erro sem chamar o `Runner`
- [x] Erro ou prazo estourado do `systemd-run` vira erro com motivo (para o log)
- [x] Gate passa: `go test -race -count=1 ./internal/terminal/`, com `TestOpenMontaComandoFixo`, `TestOpenRotuloSemCaractereDeControle`, `TestOpenErroDoSystemdRun` (+ os do T3 e T6)

**Feito** (3c58667).

**Tests**: unit · **Gate**: quick
**Commit**: `feat(terminal): abre janela do Ptyxis presa ao pane`

---

### T8: `SetTerminal`, `trustedPeer` e `focusAllowed` [P]

**What**: Interface `web.Terminal` (`Locate`/`Open`, com a `Location` vista pelo `web`), `SetTerminal(allowed []netip.Addr, t Terminal)`, `terminalOff` (404 sem `SetTerminal`), `trustedPeer(r)` (loopback, IP de interface local por `net.InterfaceAddrs`, IP da lista; `Unmap`, comparação exata) e `focusAllowed(w, r)` (403/415/403/403 como no design).
**Where**: `internal/web/server.go` (ou `internal/web/terminal.go`), `internal/web/terminal_test.go`
**Depends on**: None
**Reuses**: `crossSiteSubresource`, `remoteIsLoopback`, padrão de `SetPush`/`pushOff`
**Requirement**: FOCO-06, FOCO-10

**Done when**:

- [x] `trustedPeer`: loopback v4/v6, IP local, IP da lista, IPv4 mapeado em IPv6 aceitos; IP fora recusado
- [x] `focusAllowed`: sem JSON → 415; sem `Sec-Fetch-Site: same-origin`, com `Origin` de outro host ou sem `Origin` → 403
- [x] Gate passa: `go test -race -count=1 ./internal/web/`, com ≥ 2 testes novos em tabela (`TestTrustedPeer`, `TestFocusAllowed`)

**Feito** (c1c647e). Sem `SetTerminal`, as rotas respondem 404 (`terminalOff` virou checagem em cada handler).

**Tests**: unit · **Gate**: quick
**Commit**: `feat(web): regra de aparelho autorizado para abrir o terminal`

---

### T9: `GET /api/session/{id}/terminal`

**What**: Rota autenticada que acha o transcript da sessão (`Store.TranscriptPath`), deduz a conta (`account.ConfigDir`), chama `Locate` e responde `{estado, rotulo, podeAbrir}`; `podeAbrir` = estado `tmux` e `trustedPeer`.
**Where**: `internal/web/server.go` (rotas), `internal/web/terminal.go`, `internal/web/terminal_test.go`
**Depends on**: T1, T2, T8
**Requirement**: FOCO-02, FOCO-03, FOCO-04

**Done when**:

- [x] Sem token → 401 (mesmo `s.auth`); sessão desconhecida → 404; sem `SetTerminal` → 404
- [x] Resposta tem só `estado`, `rotulo`, `podeAbrir` (nada de PID, `%N`, caminho)
- [x] Gate passa: `go test -race -count=1 ./internal/web/`, com `TestTerminalPodeAbrirSoAutorizado` e `TestTerminalSessaoDesconhecida404`

**Feito** (c1c647e).

**Tests**: unit · **Gate**: quick
**Commit**: `feat(web): rota com a localização da sessão no tmux`

---

### T10: `POST /api/session/{id}/terminal/abrir`

**What**: Rota de ação: `s.auth` + `focusAllowed`, `Locate` refeito na hora, limite de 1 pedido aceito por sessão a cada 3 s (mapa sob mutex, limpo ao vencer), `Open`; 204/409/429/502/404/403 como no design, motivo detalhado só no log (sem conteúdo, AD-003).
**Where**: `internal/web/terminal.go`, `internal/web/terminal_test.go`
**Depends on**: T9
**Requirement**: FOCO-05, FOCO-06, FOCO-07

**Done when**:

- [x] `TestAbrirRecusaOutroAparelho`, `…AceitaIPDeFocusFrom`, `…SemSameOrigin`, `…SemJSON`, `…OriginDeOutroHost`, `…SegundoPedidoEm3sRecebe429`, `…SessaoEncerrada409`, `…FalhaDoOpen502` passam com `Terminal` falso
- [x] Pedido recusado (403/409/429) não chama `Open`
- [x] Gate passa: `go test -race -count=1 ./internal/web/` (+ todos os anteriores do pacote)

**Feito** (c1c647e).

**Tests**: unit · **Gate**: quick
**Commit**: `feat(web): rota que abre o terminal da sessão`

---

### T11: Flag `-focus-from` e ligação no `main` [P]

**What**: Flag repetível com lista separada por vírgula, parseada com `netip.ParseAddr`; entrada inválida aborta a subida citando a entrada; log `terminal liberado para: …` (ou `só a própria máquina`); `srv.SetTerminal(ips, terminal.New())`.
**Where**: `cmd/claude-fleet/main.go`, `cmd/claude-fleet/main_test.go`, `README.md` (seção de flags)
**Depends on**: T7, T10
**Reuses**: padrão do `-account` repetível
**Requirement**: FOCO-10

**Done when**:

- [x] `TestFocusFromRecusaEntradaInvalida` (nome, CIDR, item vazio) e `TestFocusFromAceitaListaERepeticao` passam
- [x] README documenta a flag e o `CLAUDE_FLEET_ARGS`
- [x] Gate passa: full (`gofmt`, `go vet`, `go test -race`, `bash hook/test.sh` inalterado)

**Feito** (b2df8c6). Log: `terminal liberado para: a própria máquina e <IPs>` ou `só a própria máquina`.

**Tests**: unit · **Gate**: full
**Commit**: `feat: flag -focus-from libera o terminal para outros aparelhos`

---

### T12: Pop-up com localização e ícone de terminal [P]

**What**: Pelo agente `ui-figma`: `fetch` único a `/terminal` em `abrirDetalhe`; localização em `det-sub` em todo aparelho; com `podeAbrir`, botão com ícone `terminal` em `.det-acoes`, antes de Silenciar/Ocultar, com `aria-label`/`title` "Abrir terminal (tmux S:J.P)"; clique desabilita o botão, faz o POST, mostra "Terminal aberto: …" ou o motivo em `det.aviso`, reabilita. Figma sincronizado no mesmo PR.
**Where**: `internal/web/static/index.html`, arquivo do Figma "claude-fleet — painel v2"
**Depends on**: T10
**Reuses**: `.ocultar`, `icone()`, `det.aviso`, tokens de `:root`
**Requirement**: FOCO-02, FOCO-04, FOCO-07, FOCO-09

**Tools**:

- Agent: `ui-figma`
- MCP: Figma (pelo agente)

**Done when**:

- [ ] Sem rede externa, sem build, só tokens de `:root`, alvo de toque ≥ `--toque-min`, sem animação nova
- [ ] Resposta lenta ou com erro de `/terminal` não quebra o pop-up (sem localização, sem botão)
- [ ] Gate browser: desktop e celular (vertical e horizontal); sessão em tmux, fora do tmux, local desconhecido, aparelho sem permissão (texto sem botão), pop-up de subagente
- [ ] Figma atualizado com o botão e a linha de localização

**Tests**: browser · **Gate**: browser + quick
**Commit**: `feat(ui): ícone no pop-up abre o terminal da sessão`

---

### T13: `STATE.md`, `ROADMAP.md`, spec e `uat.md`

**What**: AD-010 (texto do design), L-009 (`PrivateTmp` sem efeito na unit de usuário) e L-010 (Wayland só traz janela nova para a frente), ideia adiada "extensão do GNOME Shell para trazer a janela existente"; entrada no `ROADMAP.md`; spec com status atualizado e rastreabilidade FOCO-01..10; `uat.md` com os roteiros do desktop e do celular.
**Where**: `.specs/project/STATE.md`, `.specs/project/ROADMAP.md`, `.specs/features/focar-terminal/spec.md`, `.specs/features/focar-terminal/uat.md`
**Depends on**: T11, T12
**Requirement**: FOCO-09

**Done when**:

- [ ] AD-010 e L-009 no formato das anteriores
- [ ] Toda linha FOCO-xx com status e task
- [ ] `uat.md` cobre: desktop em sessão escondida atrás de outro programa, celular listado, celular não listado, sessão encerrada, clique duplo em < 3 s

**Tests**: none (documentação) · **Gate**: none
**Commit**: `docs(specs): decisão e UAT do focar-terminal`

---

### T14: UAT real, revisão e PR

**What**: Instalar o binário e a unit, configurar `-focus-from` com o IP do Tailscale do celular (do `tailscale status`, informado pelo usuário), rodar o `uat.md` com o usuário, rodar o agente `reviewer`, corrigir achados, abrir o PR em português.
**Where**: `~/.local/bin/claude-fleet`, `~/.config/systemd/user/claude-fleet.service`, `~/.config/claude-fleet/env`, GitHub
**Depends on**: T5, T13
**Requirement**: todos

**Tools**:

- Agent: `reviewer`

**Done when**:

- [ ] Gate full passa e `govulncheck` sem CVE alcançável
- [ ] UAT aprovado pelo usuário (janela na frente em ≤ 5 s pelo desktop; pedido pelo celular em ≤ 2 s)
- [ ] `reviewer` sem achado aberto
- [ ] PR aberto com `gh pr create`, sem linha de atribuição a ferramenta

**Tests**: real · **Gate**: full + real
**Commit**: nenhum próprio (correções do `reviewer` em commits `fix(…)` à parte)

---

## Validação das tasks

### Granularidade

| Task | Escopo | Status |
| --- | --- | --- |
| T1 | 1 função exportada | ✅ |
| T2 | 1 getter | ✅ |
| T3 | 1 interface + 1 implementação, mesmo arquivo | ✅ |
| T4 | 1 verificação manual | ✅ |
| T5 | 1 linha de unit | ✅ |
| T6 | 1 função (`Locate`) | ✅ |
| T7 | 1 função (`Open`) | ✅ |
| T8 | 1 setter + 2 regras de acesso coesas | ⚠️ coeso, mesmo arquivo |
| T9 | 1 rota | ✅ |
| T10 | 1 rota | ✅ |
| T11 | 1 flag + ligação | ✅ |
| T12 | 1 componente (botão + linha do pop-up) | ✅ |
| T13 | documentação da feature | ✅ |
| T14 | entrega | ✅ |

### Diagrama × dependências

| Task | Depends on (corpo) | Diagrama | Status |
| --- | --- | --- | --- |
| T1–T5, T8 | None | Phase 1, sem setas | ✅ |
| T6 | T3 | T3 → T6 | ✅ |
| T7 | T3, T4 | T6 → T7 (T6 depende de T3), T4 → T7 | ✅ |
| T9 | T1, T2, T8 | T1, T2, T8 → T9 | ✅ |
| T10 | T9 | T9 → T10 | ✅ |
| T11 | T7, T10 | T7, T10 → T11 | ✅ |
| T12 | T10 | T10 → T12 | ✅ |
| T13 | T11, T12 | T11, T12 → T13 | ✅ |
| T14 | T5, T13 | T13 → T14, T5 → T14 | ✅ |

T7 está no diagrama depois de T6 por compartilhar `terminal.go`; não roda em paralelo com ele.

### Testes junto do código

| Task | Camada | Exigido | Task diz | Status |
| --- | --- | --- | --- | --- |
| T1 | pacote Go | unit | unit | ✅ |
| T2 | pacote Go | unit | unit | ✅ |
| T3 | pacote Go | unit | unit | ✅ |
| T4 | verificação manual | none | none | ✅ |
| T5 | unit systemd | real | real (gate) | ✅ |
| T6, T7 | pacote Go | unit | unit | ✅ |
| T8–T10 | HTTP | unit (`httptest`) | unit | ✅ |
| T11 | `main` | unit + full | unit + full | ✅ |
| T12 | frontend | browser | browser | ✅ |
| T13 | docs | none | none | ✅ |
| T14 | entrega | real | real | ✅ |
