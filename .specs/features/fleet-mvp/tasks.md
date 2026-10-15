# fleet-mvp Tasks

**Design**: `.specs/features/fleet-mvp/design.md`
**Status**: Approved (2026-10-15) — em execução

---

## Execution Plan

### Phase 0: Spike (Sequential, antes de aprovar o design)

```
T1
```

### Phase 1: Foundation (Sequential)

```
T2
```

### Phase 2: Núcleos independentes (Parallel OK)

```
     ┌→ T3 [P]
T2 ──┼→ T4 [P]
     └→ T6 [P]
```

### Phase 3: Dependentes diretos (Parallel OK)

```
T4 ──→ T5 [P]
T6 ──→ T7 [P]
```

### Phase 4: Store completo (Parallel OK)

```
T7 ─────────→ T8  [P]
T3, T5, T7 ─→ T9  [P]
T7 ─────────→ T10 [P]
T5, T7 ─────→ T16 [P]   (P2)
```

### Phase 5: Servidor (Sequential)

```
T6, T7, T8, T10 ──→ T11
```

### Phase 6: Bordas (Parallel OK)

```
T11 ──┬→ T12 [P]
      └→ T13 [P]
```

### Phase 7: Detalhe e instalação (Parallel OK)

```
T9, T13 ──→ T14 [P]
T12 ──────→ T15 [P]   (P2)
```

### Phase 8: Fechamento (Sequential)

```
T14, T15 ──→ T17 ──→ T18
T8, T16 ───────────→ T18
```

---

## Task Breakdown

### T1: Spike — mapear o payload real de cada evento de hook

**What**: Registrar temporariamente um hook que grava **apenas os nomes das chaves** (nunca os valores) do JSON de cada evento, usar uma sessão com um subagente e anotar o resultado na seção Research do `design.md`.
**Where**: script descartável em `/tmp` + edição de `.specs/features/fleet-mvp/design.md`
**Depends on**: None
**Reuses**: —
**Requirement**: HOOK-02, STATE-02 (valida premissas)

**Tools**: Skill `update-config` (registrar e depois remover o hook temporário)

**Done when**:

- [x] Tabela "evento → chaves recebidas" registrada no design (13 eventos observados; `Notification`, `PermissionDenied` e `StopFailure` não disparam em `-p`)
- [x] Respondido: `PreToolUse` dentro de subagente traz `agent_id`? `SubagentStop` traz `agent_transcript_path`? (sim e sim)
- [x] Respondido: o que `async: true` faz; `--resume` troca o `session_id`? (não bloqueia, mas pode perder o fim; mantém o ID)
- [x] Hook temporário removido; `settings.json` igual ao de antes (os hooks ficaram num `--settings` à parte; SHA-256 do global idêntico)

**Tests**: none (spike, sem código mantido)
**Gate**: none

---

### T2: Esqueleto do módulo e validação do bind

**What**: `go mod init`, `cmd/claude-fleet/main.go` com as flags `-addr`/`-token`/`-projects` e a função `validateBind(addr, token) error`.
**Where**: `go.mod`, `cmd/claude-fleet/main.go`, `cmd/claude-fleet/main_test.go`
**Depends on**: None
**Requirement**: SEC-01, SEC-02

**Done when**:

- [x] Sem flags → bind `127.0.0.1:7777`
- [x] `0.0.0.0:7777` sem token → erro; com token → ok; `[::1]` e `127.x` contam como loopback
- [x] Gate passa: `go test ./...` (≥ 5 casos) — 21 casos

**Tests**: unit · **Gate**: quick
**Commit**: `feat(cli): esqueleto e validação de bind`

---

### T3: Tabela de preços e cálculo de custo [P]

**What**: `pricing.Cost(model, Usage)` com casamento por prefixo, cache write 1.25×/2×, fast 2× e `known=false` para modelo sem preço.
**Where**: `internal/pricing/pricing.go`, `pricing_test.go`
**Depends on**: T2
**Requirement**: TOK-03

**Done when**:

- [x] `claude-opus-5-5` não casa com a linha de `claude-opus-5` (teste de prefixo)
- [x] Caso conhecido: 1M input + 1M output de Opus 5.5 = US$ 24.00; fast = US$ 48.00
- [x] `<synthetic>` → `known=false`
- [x] Gate passa: `go test ./...` (≥ 8 casos)

**Tests**: unit · **Gate**: quick
**Commit**: `feat(pricing): custo estimado por modelo`

---

### T4: Leitor incremental de transcript [P]

**What**: `transcript.File` com `Refresh()` por offset, dedup por `message.id` (último vence), totais por categoria e por modelo, primeiro e último timestamp.
**Where**: `internal/transcript/reader.go`, `reader_test.go`, `testdata/*.jsonl`
**Depends on**: T2
**Requirement**: TOK-01

**Done when**:

- [x] Fixture com `message.id` repetido conta uma vez
- [x] Linha final incompleta não avança o offset; após completar a linha, é contada
- [x] Linha inválida é pulada e aumenta `ParseErrors`
- [x] Segundo `Refresh()` sem mudança no arquivo não relê nada (offset estável)
- [x] Gate passa: `go test ./...` (≥ 6 casos)

**Tests**: unit · **Gate**: quick
**Commit**: `feat(transcript): leitura incremental com dedup`

---

### T5: Descoberta de transcripts de subagentes

**What**: `SubagentsOf(transcriptPath)` lista `<sid>/subagents/agent-*.jsonl` e lê `agentType`/`description` do `.meta.json`.
**Where**: `internal/transcript/subagents.go`, `subagents_test.go`, `testdata/`
**Depends on**: T4
**Requirement**: TOK-02

**Done when**:

- [x] Fixture com 2 subagentes → 2 entradas com tipo e descrição
- [x] `.meta.json` ausente ou inválido → entrada sem descrição, sem erro fatal
- [x] Diretório inexistente → lista vazia
- [x] Gate passa: `go test ./...`

**Tests**: unit · **Gate**: quick
**Commit**: `feat(transcript): subagentes e metadados`

---

### T6: Parser e sanitização do evento [P]

**What**: `ingest.Parse(r, maxBytes)` decodificando só a lista branca de campos em `Event`.
**Where**: `internal/ingest/event.go`, `event_test.go`
**Depends on**: T2
**Requirement**: INGEST-02, SEC-03

**Done when**:

- [x] Payload com `prompt`, `tool_input`, `tool_response`, `message`, `last_assistant_message`, `error`, `compact_summary`, `custom_instructions`, `permission_suggestions` → `Event` sem nenhum desses valores (teste procura a string-sentinela na struct serializada)
- [x] Fixtures em `testdata/` com o formato de chaves registrado no spike T1 (design → "Spike T1")
- [x] Corpo > `maxBytes` → erro específico
- [x] `hook_event_name` ausente → erro
- [x] Gate passa: `go test ./...`

**Tests**: unit · **Gate**: quick
**Commit**: `feat(ingest): evento com lista branca`

---

### T7: Store — máquina de estados [P]

**What**: `Store.Apply(ev, now)` com as transições do design para sessão e subagente, criação implícita de sessão e `Subscribe()`.
**Where**: `internal/store/store.go`, `store_test.go`
**Depends on**: T6
**Requirement**: STATE-01, STATE-02, STATE-04

**Done when**:

- [x] Tabela de testes cobre todas as setas do diagrama de estados
- [x] Evento com `agent_id` altera só o subagente (e `LastEventAt` da sessão)
- [x] Evento de sessão desconhecida cria a sessão
- [x] `PostToolBatch` tira de **aguardando** (sessão e subagente)
- [x] `SubagentStop` de agente desconhecido é ignorado; Pre/Post da ferramenta `Agent` não muda status de subagente
- [x] `SessionStart` com `source=compact`/`resume` em sessão conhecida não altera o status
- [x] `Subscribe()` recebe sinal a cada mudança, sem bloquear com assinante lento
- [x] Gate passa: `go test -race ./...`

**Tests**: unit · **Gate**: full
**Commit**: `feat(store): estados de sessões e subagentes`

---

### T8: Store — limpeza [P]

**What**: `Store.Prune(now)`: remove encerradas há > 15 min e sem eventos há > 12 h.
**Where**: `internal/store/prune.go`, `prune_test.go`
**Depends on**: T7
**Requirement**: STATE-03

**Done when**:

- [x] Relógio injetado cobre as duas regras e os limites (14:59 fica, 15:01 sai)
- [x] Gate passa: `go test ./...`

**Tests**: unit · **Gate**: quick
**Commit**: `feat(store): limpeza de sessões antigas`

---

### T9: Refresher de tokens [P]

**What**: Laço a cada 5 s que atualiza `Main`, `Subagents[*].Usage` e `Models` das sessões não encerradas (e uma última vez ao encerrar), calculando custo via Pricing.
**Where**: `internal/store/refresher.go`, `refresher_test.go`
**Depends on**: T3, T5, T7
**Requirement**: TOK-03, TOK-04

**Done when**:

- [x] Fixture de sessão com 2 subagentes: `Total = Main + Σ subagentes` (tokens e custo)
- [x] Subagente presente no disco mas sem evento recebido aparece mesmo assim (status `concluido`)
- [x] Transcript ausente → `transcript_missing=true`, sem pânico
- [x] Gate passa: `go test -race ./...`

**Tests**: unit · **Gate**: full
**Commit**: `feat(store): tokens e custo a partir dos transcripts`

---

### T10: SSE Hub [P]

**What**: `GET /stream` envia snapshot ao conectar, a cada mudança (coalescido em 250 ms) e heartbeat a cada 20 s.
**Where**: `internal/web/sse.go`, `sse_test.go`
**Depends on**: T7
**Requirement**: UI-03

**Done when**:

- [x] Teste `httptest` recebe snapshot inicial e um novo após `Apply`
- [x] 10 mudanças em 100 ms geram ≤ 2 envios
- [x] Cliente desconectado é removido (sem goroutine vazando — conferir com `-race`)
- [x] Gate passa: `go test -race ./...`

**Tests**: integration · **Gate**: full
**Commit**: `feat(web): stream SSE`

---

### T11: Servidor HTTP, rotas e autenticação

**What**: Montar as rotas `/event`, `/`, `/stream`, `/api/sessions`, `/api/session/{id}`, o middleware de token/cookie e ligar Store, Refresher, Prune (ticker de 1 min) e Hub no `main`.
**Where**: `internal/web/server.go`, `server_test.go`, `cmd/claude-fleet/main.go`
**Depends on**: T6, T7, T8, T10
**Requirement**: INGEST-01, SEC-02, SEC-03

**Done when**:

- [x] `/event` com `RemoteAddr` não-loopback → 403; loopback → 204
- [x] Com token: sem credencial → 401; `?t=` correto → 200 + cookie `HttpOnly; SameSite=Strict`; cookie → 200
- [x] `/api/session/{id}` inexistente → 404
- [x] Nenhuma resposta contém a string-sentinela de um prompt enviado em `/event`
- [x] Gate passa: `go test -race ./...` e `go vet ./... && go build ./...`

**Tests**: integration · **Gate**: full
**Commit**: `feat(web): servidor, rotas e autenticação`

---

### T12: Script do hook [P]

**What**: `hook/fleet-hook.sh` repassando stdin com `curl -s -m 1`, sempre `exit 0`, sem saída.
**Where**: `hook/fleet-hook.sh`, `hook/test.sh`
**Depends on**: T11
**Requirement**: HOOK-01

**Done when**:

- [x] Servidor ligado: evento chega (204) e altera o estado
- [x] Servidor desligado: script termina em ≤ 1.1 s, exit 0, stdout/stderr vazios
- [x] `hook/test.sh` automatiza os dois casos

**Tests**: integration · **Gate**: `bash hook/test.sh`
**Commit**: `feat(hook): script de envio de eventos`

---

### T13: Página — grade de tiles [P]

**What**: `internal/web/static/index.html` embutida: `EventSource`, grade responsiva, tile (projeto, id curto, status, última ferramenta, custo resumido), mini-tiles de subagentes, pulso CSS, indicador "reconectando…".
**Where**: `internal/web/static/index.html`
**Depends on**: T11
**Requirement**: UI-01, UI-02, UI-03, UI-04

**Done when**:

- [x] Em 360 px sem scroll horizontal (captura headless do Firefox; confirmar no celular no UAT)
- [ ] Os 5 estados aparecem com o visual do spec ao simular eventos por `curl`
- [ ] `prefers-reduced-motion` desliga o pulso
- [ ] Derrubar e subir o servidor → página reconecta sozinha
- [x] Gate passa: `go vet ./... && go build ./...`

**Tests**: manual (UAT) · **Gate**: build
**Commit**: `feat(ui): grade de agentes ao vivo`

---

### T14: Página — modal de detalhes [P]

**What**: `<dialog>` aberto pelo tile (ou mini-tile, já focado no subagente) com status, tempo de vida ao vivo, última atividade, modelos, tabela de tokens (5 categorias × principal/subagentes/total), custo com rótulo de estimativa e lista de subagentes; polling de `/api/session/{id}` a cada 5 s enquanto aberto.
**Where**: `internal/web/static/index.html`
**Depends on**: T9, T13
**Requirement**: DET-01, DET-02, DET-03, DET-04, DET-05, DET-06

**Done when**:

- [x] Abre sem mudar a URL; fecha com X, Esc e toque fora
- [x] Números de tokens = script de conferência independente (diferença 0) numa sessão real encerrada — sessão do spike T1 + 1 subagente, 5 categorias com diferença 0
- [x] Modelo sem preço mostra "n/d"; transcript ausente mostra "transcript indisponível"
- [ ] Tempo de vida avança a cada segundo; tokens atualizam ≤ 5 s com a sessão ativa
- [x] Gate passa: `go vet ./... && go build ./...`

**Tests**: manual (UAT) · **Gate**: build
**Commit**: `feat(ui): pop-up de detalhes do agente`

---

### T15: Instalador de hooks (P2) [P]

**What**: Subcomandos `install-hooks` e `uninstall-hooks`: backup datado, merge preservando entradas alheias, diff, confirmação, idempotência.
**Where**: `cmd/claude-fleet/install.go`, `install_test.go`, `testdata/settings-*.json`
**Depends on**: T12
**Requirement**: HOOK-02, INST-01

**Done when**:

- [x] Fixture igual ao `settings.json` atual (telegram-notify, bws-guard) → depois do install, as entradas antigas continuam idênticas
- [x] Install duas vezes → segunda execução diz "nada a fazer"
- [x] Uninstall remove só as entradas do claude-fleet
- [x] Gate passa: `go test ./...`

**Tests**: unit · **Gate**: quick
**Commit**: `feat(cli): instalador de hooks`

---

### T16: Descoberta de sessões no boot (P2) [P]

**What**: Ao iniciar, varrer `<projects>/*/*.jsonl` modificados há ≤ 30 min e criar sessões com status `desconhecido`.
**Where**: `internal/store/bootstrap.go`, `bootstrap_test.go`
**Depends on**: T5, T7
**Requirement**: BOOT-01

**Done when**:

- [x] Arquivo com `mtime` de 10 min → sessão criada; 40 min → ignorado
- [x] Primeiro evento real tira a sessão de `desconhecido`
- [x] Gate passa: `go test ./...`

**Tests**: unit · **Gate**: quick
**Commit**: `feat(store): descoberta de sessões no boot`

---

### T17: README e operação

**What**: README com build, instalação dos hooks (manual e via T15), uso com Tailscale (`tailscale serve` ou bind no IP da tailnet + token), exemplo de unit systemd de usuário e o aviso sobre custo estimado.
**Where**: `README.md`, `deploy/claude-fleet.service`
**Depends on**: T14, T15
**Requirement**: HOOK-02, SEC-02

**Done when**:

- [ ] Seguindo só o README numa shell limpa, o painel sobe e recebe eventos

**Tests**: none (documentação) · **Gate**: build
**Commit**: `docs: instalação e acesso pelo celular`

---

### T18: UAT de ponta a ponta

**What**: Validar os Success Criteria do spec no ambiente real: 2 terminais + 1 subagente em background, acesso pelo celular via Tailscale.
**Where**: `.specs/features/fleet-mvp/` (relatório de validação)
**Depends on**: T8, T14, T15, T16, T17
**Requirement**: todos os P1

**Done when**:

- [ ] Cada critério P1 marcado como verificado ou com desvio registrado
- [ ] Latência do hook com o servidor desligado medida (≤ 1 s)
- [ ] Busca pela string-sentinela de prompt no log, na API e na página → nada

**Tests**: manual (UAT) · **Gate**: full

---

## Parallel Execution Map

```
Phase 0:  T1
Phase 1:  T2
Phase 2:  T3 [P] · T4 [P] · T6 [P]
Phase 3:  T5 [P] · T7 [P]
Phase 4:  T8 [P] · T9 [P] · T10 [P] · T16 [P]
Phase 5:  T11
Phase 6:  T12 [P] · T13 [P]
Phase 7:  T14 [P] · T15 [P]
Phase 8:  T17 → T18
```

---

## Validação pré-aprovação

### Check 1 — Granularidade

| Task | Escopo | Status |
| --- | --- | --- |
| T1 | 1 spike, 1 seção de doc | ✅ |
| T2 | 1 arquivo + 1 função de validação | ✅ |
| T3 | 1 função | ✅ |
| T4 | 1 tipo (`File`) | ✅ |
| T5 | 1 função | ✅ |
| T6 | 1 função | ✅ |
| T7 | 1 método central do Store | ✅ |
| T8 | 1 método | ✅ |
| T9 | 1 laço | ✅ |
| T10 | 1 handler | ✅ |
| T11 | rotas + middleware no mesmo pacote | ⚠️ coeso (mesmo arquivo de servidor) |
| T12 | 1 script | ✅ |
| T13 | 1 view (grade) | ✅ |
| T14 | 1 componente (modal) | ✅ |
| T15 | 1 subcomando par (install/uninstall) | ⚠️ coeso |
| T16 | 1 função | ✅ |
| T17 | 1 doc + 1 unit de exemplo | ✅ |
| T18 | validação | ✅ |

### Check 2 — Diagrama × definição

| Task | Depends on (corpo) | Diagrama | Status |
| --- | --- | --- | --- |
| T1 | — | — | ✅ |
| T2 | — | — | ✅ |
| T3 | T2 | T2 → T3 | ✅ |
| T4 | T2 | T2 → T4 | ✅ |
| T5 | T4 | T4 → T5 | ✅ |
| T6 | T2 | T2 → T6 | ✅ |
| T7 | T6 | T6 → T7 | ✅ |
| T8 | T7 | T7 → T8 | ✅ |
| T9 | T3, T5, T7 | T3, T5, T7 → T9 | ✅ |
| T10 | T7 | T7 → T10 | ✅ |
| T11 | T6, T7, T8, T10 | T6, T7, T8, T10 → T11 | ✅ |
| T12 | T11 | T11 → T12 | ✅ |
| T13 | T11 | T11 → T13 | ✅ |
| T14 | T9, T13 | T9, T13 → T14 | ✅ |
| T15 | T12 | T12 → T15 | ✅ |
| T16 | T5, T7 | T5, T7 → T16 | ✅ |
| T17 | T14, T15 | T14, T15 → T17 | ✅ |
| T18 | T8, T14, T15, T16, T17 | T8, T16, T17 → T18 (T14 e T15 via T17) | ✅ |

Tarefas `[P]` da mesma fase não dependem entre si (Phase 4: T8/T9/T10/T16 só dependem de fases anteriores).

### Check 3 — Testes co-localizados (matriz proposta no design)

| Task | Camada | Matriz exige | Task diz | Status |
| --- | --- | --- | --- | --- |
| T1 | spike | none | none | ✅ |
| T2 | cmd | unit | unit | ✅ |
| T3 | pricing | unit | unit | ✅ |
| T4 | transcript | unit | unit | ✅ |
| T5 | transcript | unit | unit | ✅ |
| T6 | ingest | unit | unit | ✅ |
| T7 | store | unit | unit | ✅ |
| T8 | store | unit | unit | ✅ |
| T9 | store | unit | unit | ✅ |
| T10 | web | integration | integration | ✅ |
| T11 | web | integration | integration | ✅ |
| T12 | hook | integration | integration | ✅ |
| T13 | página | manual | manual | ✅ |
| T14 | página | manual | manual | ✅ |
| T15 | cmd | unit | unit | ✅ |
| T16 | store | unit | unit | ✅ |
| T17 | doc | none | none | ✅ |
| T18 | UAT | manual | manual | ✅ |

---

## Ferramentas por tarefa (confirmar antes do Execute)

- **Skills:** `update-config` (T1, e para revisar o `settings.json` no T15), `run` (T13/T14 — ver a página rodando), `claude-in-chrome` (opcional, T13/T14 em viewport de 360 px), `git-commit` (um commit por tarefa, sem linha de atribuição).
- **MCPs:** nenhum necessário.
