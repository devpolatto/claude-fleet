# State

**Last Updated:** 2026-10-15
**Current Work:** fleet-mvp — planejamento concluído (spec, design e tasks em Draft); construção adiada

---

## Recent Decisions (Last 60 days)

### AD-001: Go com stdlib e página embutida (2026-10-15) — PROPOSTA

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

## Active Blockers

### B-001: Tailscale não instalado na máquina

**Discovered:** 2026-10-15
**Impact:** Impede o acesso pelo celular no UAT (T18); não bloqueia o desenvolvimento local.
**Workaround:** Testar no desktop em `127.0.0.1`.
**Resolution:** Instalar o Tailscale na máquina e no celular antes do T18.

---

## Lessons Learned

### L-001: O transcript repete o mesmo `message.id` em várias linhas

**Context:** Inspeção de um transcript real (2026-10-15).
**Problem:** 76 de 127 mensagens aparecem em mais de uma linha (uma por bloco de conteúdo); somar linha a linha infla os tokens.
**Solution:** Deduplicar por `message.id`, último registro vence.
**Prevents:** Custo e tokens superestimados.

---

## Quick Tasks Completed

| # | Description | Date | Commit | Status |
| --- | --- | --- | --- | --- |

---

## Deferred Ideas

- [ ] Alerta no navegador/vibração quando um agente fica aguardando permissão — avaliar sobreposição com o hook `telegram-notify` existente — Captured during: fleet-mvp/spec
- [ ] Histórico em SQLite e custo por dia/projeto — Captured during: fleet-mvp/spec
- [ ] Detectar sessão morta sem `SessionEnd` (PID ou inatividade do transcript) — Captured during: fleet-mvp/design
- [ ] PWA (ícone na tela inicial) — Captured during: fleet-mvp/spec

---

## Todos

- [ ] Aprovar spec, design (incluindo AD-001 e a matriz de testes) e tasks antes de executar
- [ ] Rodar o spike T1 primeiro — ele pode alterar o design (campos por evento, `async`)
- [ ] Revisar a tabela de preços na data do build

---

## Preferences

**Model Guidance Shown:** never
