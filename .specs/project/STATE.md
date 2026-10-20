# State

**Last Updated:** 2026-10-20
**Current Work:** fleet-mvp concluído (T1–T18; M1 entregue em 2026-10-15). Painel v2 no ar como serviço systemd na tailnet. Próximo: M2

---

## Recent Decisions (Last 60 days)

### AD-001: Go com stdlib e página embutida (2026-10-15) — APROVADA

**Decision:** Servidor em Go 1.27 só com stdlib; frontend em HTML/JS puro via `embed`.
**Reason:** Binário único, sem runtime, fácil de virar serviço systemd; combina com o perfil DevOps do dono.
**Trade-off:** Python ficaria pronto mais rápido.
**Impact:** Estrutura `cmd/` + `internal/`; testes com `go test`.

### AD-002: Status por hooks, tokens pelo transcript (2026-10-15)

**Decision:** Os hooks dão o status em tempo real; tokens e custo vêm da leitura incremental dos transcripts JSONL.
**Reason:** O payload do hook não traz `usage`; só o hook sabe que a sessão está "aguardando permissão".
**Trade-off:** Tokens têm atraso de até 5 s.
**Impact:** Dois caminhos de dados convergindo no Store.

### AD-003: Somente leitura e sem conteúdo (2026-10-15)

**Decision:** O painel não executa ações e o servidor descarta tudo que não é metadado (lista branca).
**Reason:** Os payloads contêm prompts e comandos de trabalho.
**Trade-off:** Não dá para ver "o que" o agente está fazendo além do nome da ferramenta.
**Impact:** Sanitização é requisito P1 (INGEST-02, SEC-03).

### AD-004: Acesso remoto só por Tailscale, com token fora do loopback (2026-10-15)

**Decision:** Bind padrão `127.0.0.1`; bind em outra interface exige token.
**Reason:** Nada de porta aberta para a internet.
**Impact:** Tailscale vira pré-requisito do UAT.

### AD-005: Custo é estimativa pelo preço de lista da API (2026-10-15)

**Decision:** Exibir custo em USD calculado com a tabela de preços do design, sempre rotulado como estimativa.
**Reason:** Em plano de assinatura não existe custo por chamada; ainda assim o número serve de métrica comparativa.
**Impact:** Tabela de preços precisa de revisão quando sair modelo novo.

---

### AD-006: Hook síncrono, sem `async` (2026-10-15)

**Decision:** Manter o `curl -m 1` síncrono em todos os eventos.
**Reason:** No spike T1, `async: true` não bloqueou, mas perdeu 1 de 5 execuções quando o processo terminou. Perder um evento final deixa o tile preso.
**Trade-off:** Até 1 s de atraso por evento, e só quando o servidor está fora do ar.
**Impact:** `hook/fleet-hook.sh` e o instalador (T12, T15) não usam `async`.

---

### AD-007: Uso do plano vem do status line, não do token OAuth (2026-10-16)

**Decision:** A feature `uso-plano` lê `rate_limits` (`five_hour`, `seven_day`) do JSON que o Claude Code entrega ao comando de status line; um `fleet-statusline.sh` repassa ao `POST /statusline` (só loopback) e o servidor guarda só esses campos.
**Reason:** Recurso documentado, sem ler credencial e no mesmo caminho do hook (loopback → lista branca → Store → SSE).
**Trade-off:** O dado só se atualiza com alguma sessão interativa respondendo; ocupa o `statusLine`.
**Alternative:** Ler o token em `~/.claude/.credentials.json` e consultar `GET /api/oauth/usage` (o que o `/usage` faz): endpoint não documentado e credencial no servidor.
**Impact:** Novo script embutido, rota `/statusline`, `install-hooks` passa a cuidar do `statusLine`; segunda tela no frontend.

---

### AD-008: Segurança no CI com CodeQL e govulncheck (2026-10-17)

**Decision:** Workflow `security.yml` com CodeQL (`security-extended`; Go, JavaScript do frontend e os workflows) e `govulncheck`, em todo PR, na `main` e semanalmente. Dependabot só para as GitHub Actions.
**Reason:** Sem dependências, o risco de terceiros é a própria stdlib: na adoção o `govulncheck` achou 11 CVEs alcançáveis no Go 1.27.1 (`net/http`, `crypto/tls`, `html/template`), corrigidos no 1.27.2. O CodeQL faz análise de fluxo com poucos falsos positivos e publica na aba Security.
**Trade-off:** O `govulncheck` reprova o PR quando sai CVE novo da stdlib, mesmo sem relação com a mudança; a correção é subir o `go` do `go.mod`.
**Alternative:** Trivy (forte em imagem, IaC e dependências, que o projeto não tem; secrets ficam com o secret scanning do GitHub) e gosec (18 achados na adoção, todos falsos positivos ou `G104` em `Close` de caminho de erro).
**Impact:** `go.mod` vai para `go 1.27.2`; `.github/workflows/security.yml` e `.github/dependabot.yml` novos.

---

### AD-009: Conta do Claude derivada do diretório do transcript (2026-10-18)

**Decision:** Com várias assinaturas na mesma máquina (`CLAUDE_CONFIG_DIR` por conta), a conta de uma sessão é o diretório de configuração que contém o `transcript_path`, comparado com as contas declaradas no servidor (`-account nome=dir`). Só o nome da conta é gravado e exposto.
**Reason:** Não muda o hook nem a lista branca do `ingest` (AD-003, AD-006) e vale também para as sessões achadas pelo Bootstrap.
**Trade-off:** As contas precisam ser declaradas no serviço; diretório não declarado aparece como "outra".
**Alternative:** Hook mandar `$CLAUDE_CONFIG_DIR` (campo novo vindo do cliente, não cobre o Bootstrap); descobrir sozinho os `~/.claude*` (pega backups e diretórios abandonados).
**Impact:** Pacote `internal/account`, flag `-account`, `Session.Account`, `install-hooks` respeita `CLAUDE_CONFIG_DIR`, `uso-plano` por conta. Spec em `features/multi-conta`.

---

## Active Blockers

_Nenhum._

---

## Lessons Learned

### L-001: O transcript repete o mesmo `message.id` em várias linhas

**Context:** Inspeção de um transcript real (2026-10-15).
**Problem:** 76 de 127 mensagens aparecem em mais de uma linha (uma por bloco de conteúdo); somar linha a linha infla os tokens.
**Solution:** Deduplicar por `message.id`, último registro vence.
**Prevents:** Custo e tokens superestimados.

### L-002: Permissão recusada no modo interativo não dispara hook

**Context:** UAT do fleet-mvp (2026-10-15), sessão interativa num tmux.
**Problem:** Depois do `PermissionRequest` não chega nenhum evento quando o usuário responde "No"; o tile ficava preso em "aguardando". O spike, feito com `claude -p`, tinha visto um `PostToolBatch` e escondeu isso.
**Solution:** Usar a linha `system/turn_duration` do transcript como fim de turno quando ela é posterior ao último evento.
**Prevents:** Concluir sobre o comportamento interativo a partir de testes em `-p`.

### L-003: Sessão retomada repete mensagens de outro transcript

**Context:** Revisão do `uso-plano` (2026-10-18), soma de tokens das janelas de 5 h e 7 dias.
**Problem:** A L-001 deduplica dentro de um arquivo, mas uma sessão retomada grava num JSONL novo mensagens com o mesmo `message.id` de outro (36 por conta numa semana, ~0,5% dos tokens).
**Solution:** Quem soma vários transcripts deduplica por `message.id` no conjunto (`transcript.File.CallsSince`); mensagem sem id não é deduplicada.
**Prevents:** Tokens e custo inflados em somas por conta ou por período (vale também para o futuro histórico em SQLite).

### L-004: Qualquer `statusLine` esconde as dicas do rodapé do Claude Code

**Context:** Doc oficial do status line, conferida no design do `uso-plano` (2026-10-18).
**Problem:** Com um `statusLine` configurado, o Claude Code deixa de mostrar `esc to interrupt`, `? for shortcuts` e afins, mesmo que o script não imprima nada.
**Solution:** O `install-hooks` avisa na mensagem final; o README documenta; contas não declaradas no serviço não recebem o `statusLine`.
**Prevents:** Surpresa de quem instala o Fleet e perde as dicas sem saber por quê.

### L-005: Peer de loopback não é sinônimo de script local

**Context:** Auditoria de segurança (2026-10-20, commit `35cd73a`).
**Problem:** `POST /event` e `POST /statusline` só checavam `remoteIsLoopback`. O navegador do dono também é peer de loopback e repassa POSTs "simples" (`text/plain`, `no-cors`, sem preflight) de qualquer site: uma página aberta forjava sessões, push de "aguardando permissão" e uso do plano, inclusive com token (o listener extra em `127.0.0.1` usa o mesmo handler). Sem token, o servidor também não olhava o `Host`, o que deixava as rotas abertas ao alcance de DNS rebinding.
**Solution:** `fromLocalTool` exige `Content-Type: application/json` (415), sem `Origin` e com `Sec-Fetch-Site` ausente ou `none` (403); o curl dos scripts já manda assim. Sem token, `loopbackHost` aceita só `localhost` ou IP de loopback no `Host` (421).
**Prevents:** Tratar a origem TCP como prova de quem é o cliente em qualquer rota sem token.

### L-006: Limite de tentativas por IP vira bloqueio do dono se o navegador dele puder tentar

**Context:** Revisão do endurecimento da #25 (2026-10-20).
**Problem:** O primeiro desenho do limite de token errado (10 por minuto por IP) contava qualquer pedido. Uma página qualquer aberta no celular, com dez `<img src=".../api/sessions?t=x">`, esgotava as tentativas do IP do próprio celular e deixava o painel em 429, mesmo com o cookie certo.
**Solution:** Pedido que o navegador marca como de outro site sem ser navegação de topo (`Sec-Fetch-Site` cross-site ou same-site com `Sec-Fetch-Dest` diferente de `document`) recebe 403 sem testar nem contar o token. Navegação de topo com `?t=` continua valendo; o risco que sobra (uma página que navega a aba de topo dez vezes) foi aceito.
**Prevents:** Rate limit que um terceiro usa para negar acesso ao dono.

---

## Resolved Blockers

### B-001: Tailscale não instalado na máquina

**Discovered:** 2026-10-15
**Impact:** Impede o acesso pelo celular no UAT (T18); não bloqueia o desenvolvimento local.
**Workaround:** Testar no desktop em `127.0.0.1`.
**Resolution:** Instalar o Tailscale na máquina e no celular antes do T18.
**Status (2026-10-15):** painel já roda como serviço systemd de usuário (`claude-fleet.service`) com o drop-in `local.conf` (loopback, sem token). Ao instalar o Tailscale: criar `~/.config/claude-fleet/env`, apagar o drop-in e reiniciar o serviço.
**Resolvido (2026-10-15):** Tailscale instalado; o serviço escuta em `100.x.y.z:7777` com token, o drop-in de loopback foi removido e o acesso pelo celular foi confirmado.

---

## Quick Tasks Completed

| # | Description | Date | Commit | Status |
| --- | --- | --- | --- | --- |
| 1 | Ingest recusa requisição de navegador e, sem token, Host fora do loopback (L-005) | 2026-10-20 | branch `fix/web-ingest-origem` | Feito |
| 2 | Endurecimento da auditoria (#25): timeouts do servidor, limite de token errado por IP, token ≥ 24 fora do loopback, push sem redirect, `/api/push/test` só JSON, aspas no `install-hooks`, transcript só arquivo regular e linha ≤ 64 MiB, campo ≤ 4096 bytes, até 500 sessões | 2026-10-20 | branch `fix/endurecimento` | Feito |

---

## Deferred Ideas

- [x] Alerta no navegador/vibração quando um agente fica aguardando permissão — avaliar sobreposição com o hook `telegram-notify` existente — Captured during: fleet-mvp/spec — feito: som em `features/alerta-sonoro` e push em `features/notificacao-push` (2026-10-16); o `telegram-notify` fica (aprova à distância)
- [ ] Histórico em SQLite e custo por dia/projeto — Captured during: fleet-mvp/spec
- [ ] Detectar sessão morta sem `SessionEnd` (PID ou inatividade do transcript) — Captured during: fleet-mvp/design
- [x] Uso do plano (janela de 5 h e semanal, como o `/usage`) numa segunda tela, trocada com um dedo — Captured during: conversa de 2026-10-16 — especificada em `features/uso-plano` (2026-10-16) — feito, UAT em `features/uso-plano/uat.md` (2026-10-18)
- [ ] PWA (ícone na tela inicial) — Captured during: fleet-mvp/spec — especificada em `features/pwa` (2026-10-16)
- [x] Custo e tokens por modelo e acerto de cache no pop-up — Captured during: issue #3 (2026-10-16) — feito em `features/custo-por-modelo` (2026-10-17)
- [x] Várias assinaturas na mesma máquina (`CLAUDE_CONFIG_DIR` por conta): ver as sessões de todas e o uso de cada plano — Captured during: conversa de 2026-10-18 — spec, design e tasks aprovados em `features/multi-conta` (2026-10-18, AD-009); feito, UAT em `features/multi-conta/uat.md` (2026-10-18)
- [x] Alerta sonoro quando a sessão principal termina um turno longo — Captured during: issue #15 (2026-10-18) — feito em `features/alerta-fim` (2026-10-18), só som, limite de 20 s, mesmo toggle
- [ ] Push "terminou" pelo `internal/push/notifier.go` (fim de turno com o celular bloqueado, sem subagentes) — Captured during: alerta-fim/spec (2026-10-18)
- [x] Silenciar o alerta sonoro de um agente específico em aguardando — Captured during: issue #19 (2026-10-20) — feito em `features/silenciar-agente` (2026-10-20), só desta espera, em memória, chaves independentes
- [x] Modelo de cada subagente no card do pop-up — Captured during: conversa de 2026-10-20 — feito em `features/modelo-subagente` (2026-10-20), só frontend; versão final com a lista de modelos e custo sempre visível no card (desenho do usuário, Figma 46:466)
- [ ] Modelo do subagente no mini-tile da grade (exige `Models` no `AgentView` do SSE) — Captured during: modelo-subagente/spec (2026-10-20)
- [ ] Silêncio com prazo (5 / 15 min) — Captured during: silenciar-agente/spec (2026-10-20)
- [ ] Silenciar também o push de aguardando (exige estado no servidor e rota de escrita) — Captured during: silenciar-agente/spec (2026-10-20)

---

## Todos

- [x] Aprovar spec, design (incluindo AD-001 e a matriz de testes) e tasks antes de executar — aprovado em 2026-10-15
- [x] Rodar o spike T1 primeiro — feito em 2026-10-15; resultados em `design.md` → "Spike T1"
- [x] Incorporar ao design os 4 impactos do spike — feito em 2026-10-15 (design, spec STATE-01/02, tasks T6/T7)
- [ ] Revisar a tabela de preços na data do build

---

## Preferences

**Model Guidance Shown:** never
