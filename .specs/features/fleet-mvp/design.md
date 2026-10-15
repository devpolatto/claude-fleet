# fleet-mvp Design

**Spec**: `.specs/features/fleet-mvp/spec.md`
**Status**: Draft (aguardando aprovação)

---

## Architecture Overview

Três peças, todas locais: um **hook** minúsculo que o Claude Code chama a cada evento, um **servidor Go** que mantém o estado em memória e lê os transcripts para obter os tokens, e uma **página** embutida no binário que recebe as mudanças por SSE. O celular só enxerga a página, e só através da tailnet.

```mermaid
flowchart LR
  subgraph host["Máquina local"]
    CC["Sessões do Claude Code<br/>(terminal + subagentes)"]
    H["fleet-hook.sh<br/>curl -m 1 → 127.0.0.1"]
    subgraph srv["claude-fleet (Go)"]
      IN["/event<br/>só loopback + sanitização"]
      ST["Store<br/>máquina de estados"]
      TR["TranscriptReader<br/>incremental + dedup"]
      PR["Pricing"]
      HUB["SSE Hub"]
      API["/api/sessions<br/>/api/session/{id}"]
      WEB["index.html (embed)"]
    end
    FS[("~/.claude/projects/*/<br/>&lt;sid&gt;.jsonl<br/>&lt;sid&gt;/subagents/agent-*.jsonl + .meta.json")]
  end
  PHONE["Celular (navegador)"]

  CC -- "JSON do hook via stdin" --> H --> IN --> ST
  ST -- "mudou" --> HUB
  TR -- "lê a cada 5 s" --> FS
  TR --> PR
  TR -- "tokens/custo" --> ST
  HUB -- "SSE" --> PHONE
  API --> PHONE
  WEB --> PHONE
  PHONE -. "Tailscale (tailnet)" .- srv
```

### Fluxo de estados

```mermaid
stateDiagram-v2
  [*] --> parado: SessionStart
  [*] --> desconhecido: descoberta no boot (P2)
  desconhecido --> ativo: qualquer evento de atividade
  parado --> ativo: UserPromptSubmit / PreToolUse / PostToolUse
  ativo --> aguardando: PermissionRequest / Notification
  aguardando --> ativo: PreToolUse / PostToolUse / UserPromptSubmit
  ativo --> parado: Stop
  aguardando --> parado: Stop
  parado --> encerrado: SessionEnd
  ativo --> encerrado: SessionEnd
  aguardando --> encerrado: SessionEnd
  encerrado --> [*]: 15 min depois
```

Subagentes: `SubagentStart` → **ativo**; eventos com `agent_id` → **ativo** + `last_tool`; `PermissionRequest` com `agent_id` → **aguardando**; `SubagentStop` → **concluído**.

---

## Research — fatos verificados (2026-10-15, Claude Code 2.1.300)

| Fato | Como foi verificado |
| --- | --- |
| Os eventos `SessionStart`, `SessionEnd`, `UserPromptSubmit`, `PreToolUse`, `PostToolUse`, `PostToolUseFailure`, `PermissionRequest`, `Notification`, `Stop`, `SubagentStart`, `SubagentStop`, `PreCompact` existem | `strings` no binário do Claude Code |
| Campos `agent_id`, `agent_type`, `agent_transcript_path` e a chave `async` aparecem no binário | `strings` no binário |
| Transcript principal: `~/.claude/projects/<cwd-escapado>/<session_id>.jsonl` | inspeção do disco |
| Subagentes: `<session_id>/subagents/agent-<agent_id>.jsonl` + `agent-<agent_id>.meta.json` com `agentType`, `description`, `toolUseId`, `spawnDepth`, `requestShape` | inspeção do disco |
| Linhas `type: "assistant"` trazem `message.model`, `message.id` e `message.usage` com `input_tokens`, `cache_creation_input_tokens`, `cache_read_input_tokens`, `output_tokens`, `cache_creation.ephemeral_5m_input_tokens` / `ephemeral_1h_input_tokens`, `speed` | inspeção de um transcript |
| O mesmo `message.id` se repete em várias linhas (76 de 127 mensagens num transcript real) → é preciso deduplicar | contagem num transcript |
| Linhas dos subagentes têm `sessionId` do pai, `agentId` e `isSidechain: true` | inspeção do disco |

**⚠️ Não verificado — validar no spike T1:**

- Quais campos chegam exatamente em **cada** evento (ex.: `PreToolUse` disparado dentro de um subagente traz `agent_id`?). Os nomes existem no binário; o formato por evento não foi visto.
- A semântica de `"async": true` na definição do hook (se roda em segundo plano sem bloquear). Até confirmar, o design não depende dela: o hook usa `curl -m 1`.
- Se `--resume` mantém ou troca o `session_id`.

### Tabela de preços (USD por milhão de tokens; preço de lista da API, consultado em 2026-10-15)

| Prefixo do modelo | Input | Output | Cache read |
| --- | --- | --- | --- |
| `claude-fable-5-1`, `claude-mythos-5-1` | 10.00 | 50.00 | 0.25 |
| `claude-fable-5`, `claude-mythos-5` | 10.00 | 50.00 | 1.00 |
| `claude-opus-5-5` | 4.00 | 20.00 | 0.20 |
| `claude-opus-5`, `claude-opus-4-8`, `-4-7`, `-4-6` | 5.00 | 25.00 | 0.50 |
| `claude-sonnet-5-5`, `claude-sonnet-5` | 2.00 | 10.00 | 0.20 |
| `claude-sonnet-4-6` | 3.00 | 15.00 | 0.30 |
| `claude-haiku-5-5` (prompt ≤ 100K) | 0.10 | 0.50 | 0.01 |
| `claude-haiku-4-5` | 1.00 | 5.00 | 0.10 |

Cache write = **1.25×** o input (TTL 5 min) e **2×** o input (TTL 1 h). Fast mode = **2×**. Casamento por prefixo, do mais específico para o mais geral. O cache read de `claude-sonnet-5` e a faixa > 100K do Haiku 5.5 são aproximações — rever antes do build.

---

## Code Reuse Analysis

Projeto novo, sem código para reaproveitar. Referências externas usadas só como inspiração, sem copiar código:

| Projeto | O que aproveitar |
| --- | --- |
| `disler/claude-code-hooks-multi-agent-observability` | padrão hook → servidor → WebSocket |
| `simple10/agents-observe` | hierarquia sessão → subagente |
| `appaquet/ccmon` | estado "precisa de atenção" |

### Integration Points

| Sistema | Integração |
| --- | --- |
| `~/.claude/settings.json` | Uma entrada nova por evento, **ao lado** das existentes (`telegram-notify` em `UserPromptSubmit`/`Stop`/`PermissionRequest`; `bws-guard` em `PreToolUse` com matcher `Bash`) |
| Transcripts JSONL | Somente leitura, por offset |
| Tailscale | Não instalado na máquina hoje — pré-requisito do UAT (B-001) |

---

## Components

### HookScript

- **Purpose**: Repassar o JSON do evento ao servidor, sem nunca atrapalhar o Claude Code.
- **Location**: `hook/fleet-hook.sh`
- **Interfaces**: stdin = JSON do hook → `curl -s -m 1 --data-binary @- http://127.0.0.1:${CLAUDE_FLEET_PORT:-7777}/event`; sempre `exit 0`, sem stdout.
- **Dependencies**: `curl`
- **Reuses**: —

### Payload / Sanitizer

- **Purpose**: Converter o JSON bruto do hook num `Event` só com metadados.
- **Location**: `internal/ingest/event.go`
- **Interfaces**:
  - `Parse(r io.Reader, maxBytes int64) (Event, error)` — decodifica num struct com **lista branca** de campos; todo o resto é descartado na decodificação.
- **Dependencies**: `encoding/json`

### Store

- **Purpose**: Máquina de estados das sessões e subagentes; dona da verdade em memória.
- **Location**: `internal/store/store.go`
- **Interfaces**:
  - `Apply(ev Event, now time.Time) (changed bool)`
  - `SetUsage(sessionID string, u SessionUsage)`
  - `Snapshot() []SessionView` / `Get(id string) (SessionDetail, bool)`
  - `Prune(now time.Time) (changed bool)` — encerradas > 15 min, inativas > 12 h
  - `Subscribe() <-chan struct{}` — sinaliza mudanças para o Hub
- **Dependencies**: `sync.RWMutex`

### TranscriptReader

- **Purpose**: Ler tokens dos transcripts de forma incremental e deduplicada.
- **Location**: `internal/transcript/reader.go`
- **Interfaces**:
  - `(*File) Refresh() error` — lê do offset até a última linha completa, atualiza `map[msgID]Call`
  - `(*File) Totals() Usage` / `ByModel() map[string]Usage` / `FirstTS()`, `LastTS()`
  - `SubagentsOf(transcriptPath string) []SubagentFile` — lista `subagents/agent-*.jsonl` + `.meta.json`
- **Dependencies**: `bufio`, `encoding/json`, Pricing

### Pricing

- **Purpose**: Custo estimado de uma chamada.
- **Location**: `internal/pricing/pricing.go`
- **Interfaces**: `Cost(model string, u Usage) (usd float64, known bool)`

### Refresher

- **Purpose**: A cada 5 s, para sessões não encerradas (e uma última vez ao encerrar), atualizar tokens e custo no Store.
- **Location**: `internal/store/refresher.go`
- **Interfaces**: `Run(ctx context.Context, interval time.Duration)`
- **Dependencies**: Store, TranscriptReader

### SSE Hub

- **Purpose**: Enviar o snapshot a todos os navegadores conectados quando o Store muda (coalescido a cada 250 ms) e um heartbeat a cada 20 s.
- **Location**: `internal/web/sse.go`
- **Interfaces**: `ServeHTTP` em `GET /stream` → `event: snapshot` com `[]SessionView` em JSON.

### HTTP Server

- **Purpose**: Rotas, autenticação e validação do bind.
- **Location**: `internal/web/server.go`, `cmd/claude-fleet/main.go`
- **Rotas**:
  - `POST /event` — loopback apenas (403 fora), corpo ≤ 256 KiB, responde 204
  - `GET /` — página; `GET /stream` — SSE; `GET /api/sessions`; `GET /api/session/{id}`
- **Flags**: `-addr` (padrão `127.0.0.1:7777`), `-token` (ou `CLAUDE_FLEET_TOKEN`), `-projects` (padrão `~/.claude/projects`)
- **Auth**: com token, `?t=<token>` na primeira visita grava cookie `HttpOnly; SameSite=Strict`; comparação em tempo constante. Bind não-loopback sem token → recusa subir.

### Página (index.html)

- **Purpose**: Grade de tiles + modal de detalhes.
- **Location**: `web/index.html` (embutida via `embed`)
- **Comportamento**: `EventSource('/stream')` com reconexão nativa; grade CSS `auto-fill, minmax(150px, 1fr)`; pulso via `@keyframes` só no status ativo (respeita `prefers-reduced-motion`); modal com `<dialog>` (Esc e toque fora fecham); ao abrir, busca `/api/session/{id}` a cada 5 s enquanto aberto; tempo de vida contado no cliente a partir de `started_at`.

### Installer (P2)

- **Purpose**: `install-hooks` / `uninstall-hooks` com backup, diff e idempotência.
- **Location**: `cmd/claude-fleet/install.go`
- **Regra**: identifica as próprias entradas pelo caminho do comando (`.../fleet-hook.sh`); nunca altera outras.

---

## Data Models

```go
type Status string // "ativo" | "aguardando" | "parado" | "encerrado" | "desconhecido" | "concluido"

type Event struct { // só lista branca
    Name           string // hook_event_name
    SessionID      string
    AgentID        string // vazio = sessão principal
    AgentType      string
    ToolName       string
    Cwd            string
    TranscriptPath string
    AgentTranscriptPath string
}

type Usage struct {
    Input, Cache5m, Cache1h, CacheRead, Output int64
    CostUSD  float64
    CostKnown bool // false se algum modelo ficou sem preço
}

type Agent struct {
    ID, Type, Description string
    Status     Status
    StartedAt, LastEventAt time.Time
    EndedAt    *time.Time
    LastTool   string
    Usage      Usage
    Models     map[string]Usage
}

type Session struct {
    ID, Cwd, Project, TranscriptPath string
    Status     Status
    StartedAt, LastEventAt time.Time
    EndedAt    *time.Time
    LastTool   string
    Main       Usage              // só a sessão principal
    Models     map[string]Usage
    Subagents  map[string]*Agent
}
// SessionView (SSE) = resumo do tile; SessionDetail (modal) = Session completo + Total (Main + Σ Subagents).
```

---

## Error Handling Strategy

| Cenário | Tratamento | O que o usuário vê |
| --- | --- | --- |
| Servidor fora do ar | `curl -m 1` falha, script sai 0 em silêncio | Nada no Claude Code |
| Payload inválido ou > 256 KiB | 400/413, evento descartado, contador de erro | Nada |
| `/event` de IP não-loopback | 403 | — |
| Linha JSONL incompleta | Não avança o offset; relê no próximo ciclo | Nada |
| Linha JSONL corrompida | Pula e incrementa `parse_errors` | Contador no rodapé do modal |
| Transcript inexistente | Usage zerado, flag `transcript_missing` | "transcript indisponível" no modal |
| Modelo sem preço | Tokens contam, custo parcial | "n/d" ou "≥ $X" no custo |
| SSE caiu | `EventSource` reconecta; servidor manda snapshot completo ao conectar | Indicador "reconectando…" no topo |
| Bind público sem token | Processo sai com código 2 e mensagem | Erro no terminal |

---

## Tech Decisions

| Decisão | Escolha | Motivo |
| --- | --- | --- |
| Linguagem | Go, só stdlib | Binário único, sem runtime, fácil de virar serviço systemd |
| Tempo real | SSE em vez de WebSocket | O fluxo é só servidor → cliente; SSE reconecta sozinho e não precisa de biblioteca |
| Fonte do status | Hooks (push) | Instantâneo; o transcript sozinho não diz "aguardando permissão" |
| Fonte dos tokens | Transcript em disco (pull a cada 5 s) | O payload do hook não traz `usage`; o transcript é a fonte autoritativa e cobre subagentes antigos |
| Sanitização | Lista branca na decodificação | Campo novo no payload nunca vaza por acidente |
| Persistência | Nenhuma | MVP ao vivo; tokens são recalculáveis a partir do disco |
| Hook síncrono com `curl -m 1` | Em vez de `async: true` | `async` não foi validado (ver Research) |

---

## Testing (proposta — greenfield, confirmar antes das tarefas)

| Camada | Tipo de teste | Parallel-safe |
| --- | --- | --- |
| `internal/pricing` | unit (tabela de casos) | Sim |
| `internal/transcript` | unit com fixtures JSONL em `testdata/` | Sim |
| `internal/ingest` | unit (inclui teste de que `prompt`/`tool_input` são descartados) | Sim |
| `internal/store` | unit (transições e limpeza com relógio injetado) | Sim |
| `internal/web` | integration com `httptest` (loopback, auth, SSE) | Sim |
| `web/index.html` | manual (UAT) | — |
| `hook/fleet-hook.sh` | integration via script (servidor ligado e desligado; medir tempo) | Não (usa porta) |

**Gates:** quick = `go test ./...` · build = `go vet ./... && go build ./...` · full = quick + `go test -race ./...`
