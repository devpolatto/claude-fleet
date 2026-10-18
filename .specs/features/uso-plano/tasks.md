# uso-plano Tasks

**Design**: `.specs/features/uso-plano/design.md`
**Status**: In Progress (Phases 1–3 feitas em 2026-10-18)

**Gates** (comandos do `CLAUDE.md`; não há `.specs/codebase/TESTING.md`):

| Gate | Comando |
| --- | --- |
| quick | `go test -race -count=1 ./...` |
| full | `gofmt -l .` vazio, `go vet ./...`, `go test -race -count=1 ./...`, `bash hook/test.sh` |
| browser | Painel no desktop e em largura de celular (vertical e horizontal), com uma e com duas contas |

Os testes de Go podem rodar em paralelo (cada teste usa `t.TempDir()`). O `hook/test.sh` sobe um servidor real numa porta e por isso roda sozinho. Duas tasks que mexem no mesmo pacote Go não rodam em paralelo, mesmo em arquivos diferentes: uma edição pela metade quebra a compilação da outra.

O P1 (T1–T12) fecha a feature sozinho. O P2 (T13–T15) pode entrar no mesmo PR ou num PR seguinte.

---

## Execution Plan

### Phase 1: Fundação (Parallel OK)

```
T1 [P]    (internal/atomicfile)
T2 [P]    (fleet-statusline.sh)
T3 [P]    (ratelimit.Parse)
T10 [P]   (frontend: duas telas e troca)
T13 [P]   (P2: transcript.SumSince)
```

### Phase 2: Servidor (Sequential)

```
T1, T3 ──→ T4 ──→ T5 ──→ T6 ──→ T7
```

### Phase 3: Instalador e documentação (Parallel OK, junto com a Phase 2)

```
T2 ──→ T8 ──→ T16 [P]
```

### Phase 4: Integração

```
T7, T8 ──→ T9   (hook/test.sh)
```

### Phase 5: Interface (Sequential)

```
T7, T10 ──→ T11 ──→ T12
```

### Phase 6: P2 (Sequential)

```
T7, T13 ──→ T14 ──→ T15 ←── T12
```

### Phase 7: Fechamento

```
T9, T12, T15, T16 ──→ T17
```

---

## Task Breakdown

### T1: Pacote `internal/atomicfile` [P]

**What**: `Write(path string, data []byte, perm fs.FileMode) error` (temporário no mesmo diretório + `rename`), extraído de `push.writeFileAtomic`; o `push` passa a usar o pacote novo e a função antiga sai.
**Where**: `internal/atomicfile/atomicfile.go`, `atomicfile_test.go`; `internal/push/vapid.go`, `subscriptions.go`
**Depends on**: None
**Reuses**: corpo de `writeFileAtomic` (`internal/push/vapid.go:115`)
**Requirement**: USO-04 (pré-requisito)

**Done when**:

- [x] `Write` grava com a permissão pedida, não deixa `.tmp-*` no diretório nem em caso de erro, e troca o arquivo existente
- [x] `push` sem `writeFileAtomic`; os testes de `push` seguem passando sem mudança de comportamento
- [x] Gate passa, com `TestWriteNaoDeixaTemporario` e `TestWriteUsaPermissao` novos

**Feito** (25036e9). Sem desvio; `push` usa `atomicfile.Write(..., 0o600)`.

**Tests**: unit · **Gate**: quick
**Commit**: `refactor(push): escrita atômica no pacote atomicfile`

---

### T2: Script `fleet-statusline.sh` [P]

**What**: `hook/fleet-statusline.sh` com as regras do `fleet-hook.sh`, mandando para `/statusline`; `hook.StatusLine` embute o script; `hook/test.sh` ganha os casos do script com o servidor fora, com o servidor que nunca responde e com porta inválida.
**Where**: `hook/fleet-statusline.sh`, `hook/embed.go`, `hook/test.sh`
**Depends on**: None
**Reuses**: `hook/fleet-hook.sh` (texto e comentários), casos 2–5 do `hook/test.sh`
**Requirement**: USO-01

**Done when**:

- [x] Script com `curl -q -s -m 1 --noproxy '*'`, porta só com dígitos, sem `curl` sai 0, `[ -t 0 ]` sai 0, nunca imprime nada, sempre `exit 0`
- [x] `hook.StatusLine` não vazio e igual ao arquivo (teste em `hook/embed_test.go`)
- [x] `bash hook/test.sh`: o script novo termina em ≤ 1,1 s com o servidor fora e com o servidor que não responde; stdout e stderr vazios; exit 0
- [x] Gate passa

**Feito** (0119806). Casos 7–10 no `hook/test.sh`; o caso com o servidor ligado fica para a T9.

**Tests**: integration · **Gate**: full
**Commit**: `feat(hook): script de status line que repassa o uso do plano`

---

### T3: `ratelimit.Parse` (lista branca) [P]

**What**: `Window`, `Reading`, `Parse(r, max)` e `ErrTooLarge`, como no design: só `rate_limits.five_hour`, `rate_limits.seven_day` e `transcript_path`; janela com `used_percentage` fora de `[0, 100]` ou `resets_at <= 0` vira ausente.
**Where**: `internal/ratelimit/ratelimit.go`, `ratelimit_test.go`, `testdata/statusline-*.json`
**Depends on**: None
**Reuses**: padrão de `ingest.Parse` (`io.LimitReader`, struct só com os campos aceitos)
**Requirement**: USO-02

**Done when**:

- [x] Fixture real do JSON do status line, anonimizada (sem `cwd`, caminhos pessoais, nome de máquina, `session_id` real), mais uma sem `rate_limits` e uma só com `seven_day`
- [x] `Reading` não tem nenhum campo além das duas janelas e do `TranscriptPath`; o `TranscriptPath` não tem tag JSON
- [x] `used_percentage` decimal (`23.5`) é aceito
- [x] Gate passa, com `TestParseGuardaSoRateLimits`, `TestParseDescartaJanelaInvalida`, `TestParseRecusaCorpoGrande`

**Feito** (c445083). Desvios: parâmetro `maxBytes` (`max` é builtin); `ErrInvalidJSON` exportado, como no `ingest`; `resets_at` lido como número e truncado, para um valor decimal não derrubar o corpo; campo da lista branca com tipo errado recusa o corpo (400), como no `ingest`.

**Tests**: unit · **Gate**: quick
**Commit**: `feat(ratelimit): lista branca do JSON do status line`

---

### T4: `ratelimit.Tracker`

**What**: `Entry`, `Load(path, names, now)`, `Apply`, `Snapshot`, `Subscribe` e a gravação em `uso-plano.json`, com as regras de aviso do design (mudança de valor, ou 1 min desde o último aviso).
**Where**: `internal/ratelimit/tracker.go`, `tracker_test.go`
**Depends on**: T1, T3
**Reuses**: contrato de `Store.Subscribe` (buffer 1, envio sem bloquear), `atomicfile.Write`
**Requirement**: USO-02, USO-04

**Done when**:

- [x] Janela ausente mantém a anterior; JSON sem janela não muda nada nem mexe no `ReceivedAt`
- [x] Avisa e grava na mudança de percentual ou `resets_at`, ou quando o último aviso tem ≥ 1 min; em chamadas repetidas com o mesmo valor dentro do minuto, não avisa nem grava (relógio injetado)
- [x] Arquivo `0600`, mapa por conta; formato antigo vira a conta `""`; arquivo corrompido ou ilegível: log e começa vazio; `path` vazio: só memória
- [x] Contas de `names` aparecem no `Snapshot` sem dado; `Snapshot` ordenado e é cópia (mexer no retorno não muda o estado)
- [x] Seguro sob `-race` com `Apply` e `Snapshot` concorrentes
- [x] Gate passa, com `TestApplyJanelaAusenteMantemAnterior`, `TestApplySemJanelaNaoMuda`, `TestApplyAvisaSoNaMudancaOuDepoisDeUmMinuto`, `TestLoadFormatoAntigoViraContaImplicita`, `TestLoadArquivoCorrompidoComecaVazio`, `TestGravaComPermissao600`, `TestContasDeclaradasNascemSemDado`

**Feito** (c964fad). Decisões: `Load` com arquivo ilegível devolve o `Tracker` vazio e o erro, e o `main` loga; o `Tracker` continua tentando gravar, então a próxima leitura válida conserta um arquivo corrompido (desvio: o design dizia "só em memória"); `ReceivedAt` em memória acompanha toda leitura com janela, e aviso e gravação saem no máximo uma vez por minuto quando o valor não muda; contas sem dado não vão para o arquivo; o arquivo é considerado do formato antigo quando o topo tem `five_hour`, `seven_day` ou `received_at`.

**Tests**: unit · **Gate**: quick
**Commit**: `feat(ratelimit): último uso do plano por conta, em disco`

---

### T5: Rotas `POST /statusline` e `GET /api/uso`

**What**: `Server.SetUsage(u Usage, accounts *account.List)`; `handleStatusline` (loopback → `Parse` → `Resolve` → `Apply` → 204), contador `statuslineErrors`; `GET /api/uso` com `auth`. Sem `SetUsage`, `/statusline` responde 204 e `/api/uso` devolve `[]`.
**Where**: `internal/web/server.go`, `server_test.go`
**Depends on**: T4
**Reuses**: `handleEvent`, `remoteIsLoopback`, `writeJSON`, `auth`
**Requirement**: USO-02, USO-03

**Done when**:

- [x] Fora do loopback: 403; corpo grande: 413; JSON inválido: 400; nenhuma resposta nem log cita o corpo
- [x] Com `-account`: conta certa pelo `transcript_path`; `outra` e caminho vazio descartados. Sem contas: tudo em `""`
- [x] `/api/uso` sem token: 401; com token: `[]Entry` (lista vazia como `[]`), sem `transcript_path` nem outro campo do status line
- [x] Gate passa, com `TestStatuslineSoDoLoopback`, `TestStatuslineResolveConta`, `TestStatuslineSemUsageResponde204`, `TestApiUsoExigeToken`

**Feito** (26edb31). Desvios: `Usage` sem `Subscribe` (fica no `UsageSource` do Hub, T6); sem `SetUsage` o corpo nem é lido; limite é `ratelimit.MaxBytes`, sem constante nova no `web`.

**Tests**: unit · **Gate**: quick
**Commit**: `feat(web): rota /statusline e /api/uso`

---

### T6: Evento SSE `uso` no Hub

**What**: `NewHub(src, opts...)` com `WithUsage(UsageSource)`; `Run` escuta os dois canais com coalescência separada; ao conectar, manda `snapshot` e depois `uso`; mudança de uso manda só `uso`.
**Where**: `internal/web/sse.go`, `sse_test.go`
**Depends on**: T5
**Reuses**: `broadcast`, `snapshotEvent`, janela `Coalesce`
**Requirement**: USO-04

**Done when**:

- [x] Sem `WithUsage`, o comportamento e os testes atuais não mudam
- [x] Mudança de uso chega ao cliente em ≤ 2 s (no teste, dentro de `Coalesce` + folga) sem `snapshot` junto
- [x] Lista vazia sai como `data: []`
- [x] Gate passa, com `TestHubEnviaUsoAoConectar` e `TestHubMudancaDeUsoNaoReenviaSnapshot`

**Feito** (94372d4). Desvio: cada cliente do SSE passou a ter um canal por tipo de evento (`snapshot` e `uso`), para um `uso` não tomar o lugar de um `snapshot` ainda não lido por um cliente lento (`TestHubUsoNaoTomaOLugarDoSnapshotNoClienteLento`).

**Tests**: unit · **Gate**: quick
**Commit**: `feat(web): evento uso no SSE`

---

### T7: Ligar o `Tracker` no `main`

**What**: `run` cria o `Tracker` com `<state>/uso-plano.json` e os nomes das contas (`[""]` sem `-account`), passa ao Server (`SetUsage`) e ao Hub (`WithUsage`); erro de leitura do arquivo vira log e `Tracker` só em memória.
**Where**: `cmd/claude-fleet/main.go`, `main_test.go`
**Depends on**: T6
**Reuses**: `cfg.StateDir`, `cfg.Accounts.All()`
**Requirement**: USO-04

**Done when**:

- [x] Conferido com o binário: `curl` com a fixture do T3 em `/statusline`, valor em `/api/uso`; reiniciar o servidor e o valor continua lá; o arquivo tem `0600` e só as janelas e `received_at`
- [x] Sem `-state-dir` gravável, o servidor sobe e o uso funciona em memória
- [x] Gate passa

**Feito** (281c8b8). `loadUsage` cria o `StateDir` (0700) se faltar. Conferido com o binário: fixture por `curl`, valor em `/api/uso` e no evento `uso`, mesmo valor depois de reiniciar, arquivo `0600` sem `transcript_path`, `session_id` nem `cwd`, e nada disso no log.

**Tests**: unit · **Gate**: quick
**Commit**: `feat(cli): uso do plano ligado ao servidor`

---

### T8: `install-hooks` e `uninstall-hooks` cuidam do `statusLine`

**What**: `install-hooks` grava `<dir>/fleet-statusline.sh` (0755) e registra o `statusLine`; não toca num `statusLine` alheio e avisa; troca um `statusLine` antigo do Fleet; `uninstall-hooks` remove só o do Fleet; mensagem final sobre as dicas do rodapé.
**Where**: `cmd/claude-fleet/install.go`, `install_test.go`
**Depends on**: T2
**Reuses**: `parseObject`, `renderObject`, `sameJSON`, `syncScript`, `confirm`, `commitSettings` (diff, backup e escrita atômica já existentes)
**Requirement**: USO-05

**Done when**:

- [x] `settings.json` sem `statusLine`: ganha `{"type": "command", "command": "<dir>/fleet-statusline.sh"}`; o diff mostra a mudança antes da confirmação
- [x] `statusLine` alheio: fica byte a byte igual, aviso no stdout, hooks instalados, saída 0
- [x] Segunda execução sem mudança: "nada a fazer", sem backup novo
- [x] `uninstall-hooks` remove hooks e `statusLine` do Fleet, mantém um `statusLine` alheio
- [x] Respeita `CLAUDE_CONFIG_DIR` como os hooks (multi-conta)
- [x] Gate passa, com `TestInstallHooksRegistraStatusLine`, `TestInstallHooksNaoSobrescreveStatusLineAlheio`, `TestInstallHooksTrocaStatusLineAntigoDoFleet`, `TestUninstallHooksRemoveSoStatusLineDoFleet`, `TestInstallHooksGravaOsDoisScripts`

**Feito** (8067c8b). Escolhas: o `fleet-statusline.sh` é gravado mesmo com `statusLine` alheio, para quem quiser ligá-lo à mão; o aviso de `statusLine` alheio sai em toda execução; a mensagem final cita o comando registrado.

**Tests**: unit · **Gate**: quick
**Commit**: `feat(cli): install-hooks registra o statusLine do uso do plano`

---

### T9: Caso de ponta a ponta no `hook/test.sh`

**What**: Com o servidor de pé e duas contas (`-account`), `install-hooks -yes` em cada `CLAUDE_CONFIG_DIR`, a fixture do status line passa pelo script instalado com o `transcript_path` de cada conta, e `/api/uso` mostra o valor certo em cada uma; o `uso-plano.json` não contém `transcript_path`, `session_id` nem `cwd`.
**Where**: `hook/test.sh`
**Depends on**: T7, T8
**Reuses**: caso 6 do `test.sh` (duas contas)
**Requirement**: USO-01, USO-02, USO-05

**Done when**:

- [x] Caso novo passa, junto com os anteriores
- [x] Gate passa

**Feito** (2ac120f). Caso 11: duas contas, envio pelo script lido do `settings.json`, valor certo por conta em `/api/uso`, transcript de fora descartado, `uso-plano.json` em 600 sem caminho, `session_id` nem `cwd`. Usa python3 e grep (o `test.sh` não usa jq).

**Tests**: integration · **Gate**: full
**Commit**: `test(hook): uso do plano de duas contas pelo status line instalado`

---

### T10: Duas telas e troca com um dedo [P]

**What**: `<main>` vira `#telas` com `#tela-sessoes` e `#tela-uso` (vazia por enquanto, só o título); pontinhos `#pontos`; arrasto horizontal com Pointer Events e `touch-action: pan-y`; setas ← → fora do pop-up e de campos; `inert` na tela escondida; tela lembrada em `fleet.tela` (try/catch); sem transição em `prefers-reduced-motion`.
**Where**: `internal/web/static/index.html`
**Depends on**: None
**Reuses**: tokens de `:root`, padrão de `localStorage` do `ocultar-sessao`, bloco `@media (prefers-reduced-motion)`
**Requirement**: USO-06, USO-08
**Tools**: agente `ui-figma`

**Done when**:

- [x] Arrasto para a esquerda vai para a tela de uso e para a direita volta; rolagem vertical da grade continua; arrasto diagonal curto não troca
- [x] Não troca com o pop-up aberto nem em toque que começa no `<select>` de conta
- [x] Pontinhos com `aria-current` e toque funcionando; setas no desktop
- [x] Recarregar abre na última tela; sem `localStorage`, abre na grade
- [x] Conferido no navegador em 1280×800, 390×844 e 844×390 (no celular deitado o cabeçalho some, os pontinhos não), com e sem movimento reduzido, sem erro no console
- [x] `go test ./internal/web/` passa (a página segue embutida)

**Feito** (b3ebc11) pelo `ui-figma`, conferido com Chromium headless e toque real em 1280×800, 390×844 e 844×390. Desvios: arrasto só com toque e caneta (no desktop, setas e pontinhos); a troca acontece no fim do gesto, sem a tela acompanhar o dedo; o foco vai para o pontinho atual quando a tela dele fica `inert`; token `--tela-transicao`, zerado com movimento reduzido.

**Tests**: browser · **Gate**: browser + quick
**Commit**: `feat(ui): segunda tela trocada com um dedo`

---

### T11: Tela de uso com os medidores

**What**: Blocos por conta com os medidores **Sessão (5 h)** e **Semana** (`role="meter"`), percentual, "reinicia às HH:MM · em X h Y min", "atualizado há"; estados sem dado, reiniciada e dado velho; tokens `--uso-trilho`, `--uso-ok`, `--uso-alerta` (≥ 80%), `--uso-critico` (≥ 95%); `GET /api/uso` no carregamento e `es.addEventListener('uso', …)`; relógio de 30 s só com a tela visível.
**Where**: `internal/web/static/index.html`
**Depends on**: T7, T10
**Reuses**: `chipConta` (título com mais de uma conta), `el()`, formatadores `Intl` existentes
**Requirement**: USO-07, USO-08
**Tools**: agente `ui-figma`

**Done when**:

- [x] Com o valor injetado por `curl`: percentual igual ao enviado (arredondado), hora do reset local e contagem regressiva corretas
- [x] Uma conta: sem nome; duas contas: chip com o nome em cada bloco; conta declarada sem dado mostra o texto de "sem dado"
- [x] `resets_at` no passado mostra "reiniciada, aguardando dado novo"; cores mudam em 80% e 95%; o percentual sempre aparece em texto
- [x] `--uso-critico` com contraste ≥ 4,5:1 sobre `--surface`
- [x] Valor novo por `curl` aparece na página aberta em ≤ 2 s
- [x] Conferido em 1280×800, 390×844 e 844×390, com uma e duas contas, sem erro no console
- [x] `go test ./internal/web/` passa

**Feito** (dd0fead) pelo `ui-figma`, conferido com Playwright em 1280×800, 390×844 e 844×390, com uma e duas contas (uma sem dado); valor novo por `curl` aparece em ~270 ms. `--uso-critico` = `#f87171` (6,42:1 sobre `--surface`). Desvios: barra sem animação (o bloco é redesenhado inteiro); no estado "reiniciada" o texto toma o lugar do percentual e a linha do reset some; com uma conta o bloco só tem o "atualizado há"; token extra `--uso-barra-h`; tooltip do "atualizado há" com data e hora completas.

**Tests**: browser · **Gate**: browser + quick
**Commit**: `feat(ui): medidores do uso do plano por conta`

---

### T12: Figma da tela de uso

**What**: Tela de uso (uma e duas contas, estados sem dado e reiniciada), pontinhos e tokens `--uso-*` no "claude-fleet — painel v2", espelhando T10 e T11.
**Where**: Figma (arquivo `tVFsjlauIBbPrj2HSXNron`)
**Depends on**: T11
**Requirement**: USO-06, USO-07
**Tools**: agente `ui-figma` (MCP do Figma)

**Done when**:

- [ ] Variáveis `--uso-*` na coleção `claude-fleet · :root`
- [ ] Telas de celular (vertical e horizontal) e desktop com a tela de uso; componente dos pontinhos
- [ ] Diferenças conhecidas entre Figma e código anotadas

**Tests**: none (artefato de design) · **Gate**: comparação visual
**Commit**: nenhum no repositório (link no PR)

---

### T13: `transcript.File.SumSince` (P2) [P]

**What**: `call` guarda o `timestamp` da linha; `SumSince(t time.Time) map[ModelKey]Tokens` soma as chamadas deduplicadas a partir de `t`.
**Where**: `internal/transcript/reader.go`, `reader_test.go`
**Depends on**: None
**Reuses**: `calls` deduplicado por `message.id` (L-001)
**Requirement**: USO-09

**Done when**:

- [x] Mensagem repetida em várias linhas conta uma vez, com o timestamp da última linha
- [x] `Totals` e `ByModel` sem mudança de comportamento (testes atuais passam)
- [x] Gate passa, com `TestSumSinceRespeitaTimestampEDedupe`

**Feito** (31c2f2b). Mensagem sem timestamp válido só entra quando `t` é zero.

**Tests**: unit · **Gate**: quick
**Commit**: `feat(transcript): soma de tokens a partir de um instante`

---

### T14: Janelas de tokens e custo por conta (P2)

**What**: `ratelimit.Windows` (varre `<conta>/projects` a cada 30 s atrás de `*.jsonl` com `mtime` nos últimos 7 dias, mantém um `transcript.File` por arquivo, solta os que saem da janela), `WindowTotals`, `Tracker.SetWindows` e a ligação no `main`.
**Where**: `internal/ratelimit/windows.go`, `windows_test.go`, `testdata/projects/`; `internal/ratelimit/tracker.go`; `cmd/claude-fleet/main.go`
**Depends on**: T7, T13
**Reuses**: `transcript.File`, `pricing.Cost`, `account.Account.ProjectsDir`
**Requirement**: USO-09

**Done when**:

- [ ] Início da janela de 5 h = `five_hour.resets_at − 5 h` quando há dado do plano válido; senão `now − 5 h`
- [ ] Soma igual à contagem manual sobre as fixtures (inclui um subagente e uma mensagem repetida)
- [ ] `SetWindows` só avisa quando o total muda; as janelas não vão para o `uso-plano.json`
- [ ] Gate passa, com `TestWindowsSomaContraContagemManual` e `TestWindowsUsaInicioDaJanelaDoPlano`

**Tests**: unit · **Gate**: quick
**Commit**: `feat(ratelimit): tokens e custo das janelas de 5 h e 7 dias`

---

### T15: Tokens e custo das janelas na tela (P2)

**What**: Abaixo dos medidores, uma linha por janela com "entrada · cache · saída" e o custo com o rótulo de estimativa; Figma junto.
**Where**: `internal/web/static/index.html`; Figma
**Depends on**: T12, T14
**Reuses**: formatadores e `ROTULO_ESTIMATIVA` do pop-up
**Requirement**: USO-09
**Tools**: agente `ui-figma`

**Done when**:

- [ ] Números iguais aos de `/api/uso`; sem as janelas no JSON, a linha não aparece
- [ ] Conferido em 1280×800, 390×844 e 844×390, sem erro no console; Figma atualizado
- [ ] `go test ./internal/web/` passa

**Tests**: browser · **Gate**: browser + quick
**Commit**: `feat(ui): tokens e custo das janelas do plano`

---

### T16: README [P]

**What**: Seção curta: o `install-hooks` registra o `statusLine`, as dicas do rodapé do Claude Code somem com ele, o que acontece com um `statusLine` próprio, e onde fica o `uso-plano.json`.
**Where**: `README.md` (seções "2. Instalar os hooks" e "Como funciona")
**Depends on**: T8
**Requirement**: USO-05

**Done when**:

- [x] Texto confere com as mensagens reais do `install-hooks`

**Feito** (0079b27).

**Tests**: none (documentação) · **Gate**: leitura
**Commit**: `docs: uso do plano e statusLine no README`

---

### T17: UAT e fechamento

**What**: `uat.md` do uso-plano com as contas reais (números da tela contra o `/usage` de cada conta, diferença ≤ 1 p.p.); traceability para Verified; ROADMAP, STATE e o `README` conferidos; revisão pelo agente `reviewer`.
**Where**: `.specs/features/uso-plano/uat.md`, `spec.md`, `.specs/project/ROADMAP.md`, `STATE.md`
**Depends on**: T9, T12, T15, T16
**Requirement**: todos

**Done when**:

- [ ] Cada critério de aceitação do spec com resultado no `uat.md`
- [ ] `reviewer` sem achado aberto
- [ ] Gate full passa

**Tests**: UAT · **Gate**: full
**Commit**: `docs(specs): UAT do uso-plano`

---

## Granularity Check

| Task | Scope | Status |
| --- | --- | --- |
| T1 | 1 função extraída + troca de chamada no `push` | ⚠️ OK, mesma mudança |
| T2 | 1 script + embed + casos dele no `test.sh` | ⚠️ OK, coeso |
| T3 | 1 função (`Parse`) | ✅ |
| T4 | 1 tipo (`Tracker`) | ✅ |
| T5 | 2 rotas no mesmo arquivo | ⚠️ OK, coeso |
| T6 | 1 mudança no Hub | ✅ |
| T7 | Ligação no `main` | ✅ |
| T8 | 1 membro do `settings.json` no instalador | ✅ |
| T9 | 1 cenário de teste | ✅ |
| T10 | Troca de tela | ✅ |
| T11 | Tela de uso | ✅ |
| T12 | 1 artefato do Figma | ✅ |
| T13 | 1 método | ✅ |
| T14 | 1 tipo (`Windows`) + ligação | ⚠️ OK, coeso |
| T15 | 1 bloco da tela + Figma | ✅ |
| T16 | 1 seção de documentação | ✅ |
| T17 | Fechamento | ✅ |

## Diagram-Definition Cross-Check

| Task | Depends On (body) | Diagram Shows | Status |
| --- | --- | --- | --- |
| T1 | None | Phase 1, sem seta | ✅ |
| T2 | None | Phase 1, sem seta | ✅ |
| T3 | None | Phase 1, sem seta | ✅ |
| T4 | T1, T3 | T1, T3 → T4 | ✅ |
| T5 | T4 | T4 → T5 | ✅ |
| T6 | T5 | T5 → T6 | ✅ |
| T7 | T6 | T6 → T7 | ✅ |
| T8 | T2 | T2 → T8 | ✅ |
| T9 | T7, T8 | T7, T8 → T9 | ✅ |
| T10 | None | Phase 1, sem seta | ✅ |
| T11 | T7, T10 | T7, T10 → T11 | ✅ |
| T12 | T11 | T11 → T12 | ✅ |
| T13 | None | Phase 1, sem seta | ✅ |
| T14 | T7, T13 | T7, T13 → T14 | ✅ |
| T15 | T12, T14 | T14 → T15 ← T12 | ✅ |
| T16 | T8 | T8 → T16 | ✅ |
| T17 | T9, T12, T15, T16 | T9, T12, T15, T16 → T17 | ✅ |

Os `[P]` da Phase 1 não dependem entre si e mexem em pacotes diferentes (`atomicfile`/`push`, `hook`, `ratelimit`, `static`, `transcript`). T2 e T9 mexem no `hook/test.sh`, mas não ficam na mesma fase. T5 e T6 mexem no mesmo pacote (`web`) e por isso são sequenciais.

## Test Co-location Validation

| Task | Camada | Exigido | Task diz | Status |
| --- | --- | --- | --- | --- |
| T1 | pacote Go | unit | unit | ✅ |
| T2 | hook | integration (`test.sh`) | integration | ✅ |
| T3 | pacote Go | unit | unit | ✅ |
| T4 | pacote Go | unit | unit | ✅ |
| T5 | pacote Go | unit | unit | ✅ |
| T6 | pacote Go | unit | unit | ✅ |
| T7 | `cmd` | unit | unit | ✅ |
| T8 | `cmd` | unit | unit | ✅ |
| T9 | hook | integration | integration | ✅ |
| T10 | frontend | navegador (`CLAUDE.md`) | browser | ✅ |
| T11 | frontend | navegador | browser | ✅ |
| T12 | Figma | none | none | ✅ |
| T13 | pacote Go | unit | unit | ✅ |
| T14 | pacote Go + `cmd` | unit | unit | ✅ |
| T15 | frontend | navegador | browser | ✅ |
| T16 | docs | none | none | ✅ |
| T17 | UAT | full | full | ✅ |

## Tools

| Tasks | Ferramenta |
| --- | --- |
| T1–T9, T13, T14, T16 | Nenhum MCP; Go e shell |
| T10–T12, T15 | Agente `ui-figma` (código + Figma pelo MCP do Figma) |
| T17 | Agente `reviewer` |
