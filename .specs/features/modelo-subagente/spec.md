# modelo-subagente Specification

> Status: **implementada e verificada** em 2026-10-20. UAT em [`uat.md`](uat.md). Escopo pequeno: spec curta, design inline, sem `tasks.md`.

## Problem Statement

O card de cada subagente no pop-up mostra vida, tokens e custo, mas não o modelo. Para decidir a orquestração (que subagente vale rodar num modelo mais barato ou mais forte) preciso ver o modelo ao lado do custo, sem somar de cabeça a tabela "Modelos", que junta todos os agentes.

## Goals

- [x] Do pop-up aberto, ver o modelo de cada subagente no próprio card, sem toque extra.

## Out of Scope

| Feature | Reason |
| --- | --- |
| Campo novo na API ou no `ingest` | `Agent.Models` já vem no `GET /api/session/{id}` desde o `custo-por-modelo` |
| Modelo no mini-tile da grade | O `AgentView` do SSE não traz `Models`; o tile segue enxuto |
| Modelo pedido na chamada do Agent (`model` do `tool_input`) | `tool_input` fica fora da lista branca (AD-003); o transcript diz o modelo que de fato respondeu |
| Nome amigável ("Haiku 5.5") | Exigiria uma tabela a manter junto com a de preços |

---

## User Stories

### P1: Modelo no card do subagente ⭐ MVP

**User Story**: Como dono da máquina, quero ver no card de cada subagente o modelo que ele usou, para decidir melhor a orquestração de modelos.

**Acceptance Criteria**:

1. WHEN o transcript do subagente já foi lido THEN o card SHALL mostrar "modelo <nome>" antes de vida, tokens e custo, com o nome sem o prefixo `claude-`.
2. WHEN o subagente usou mais de um modelo THEN o card SHALL mostrar o que mais custou seguido de "+N"; empate pelo nome.
3. O `title` do nome SHALL listar todos os modelos com o nome completo e o custo de cada um, no formato do restante do pop-up (`n/d`, `≥ US$ X`).
4. WHEN o subagente ainda não tem modelo (`Models` vazio) THEN o item SHALL NOT aparecer.
5. WHEN o nome é longo demais para a largura THEN ele SHALL ser cortado com reticências, sem rolagem horizontal da página.

**Independent Test**: Servidor com `-projects` apontando para transcripts sintéticos (um subagente por modelo, um com dois modelos, um sem resposta ainda) e conferência do card em desktop e celular.

---

## Design (inline)

Tudo em `internal/web/static/index.html`; backend, API e hook não mudam.

- `modeloAgente(a)` ordena `a.Models` pelo custo (como `montarModelos`) e devolve o `<b>` do item, ou `null`.
- `montarAgente` acrescenta o item com `metaItem('modelo', …)` e a classe `.modelo`, que corta o nome com `text-overflow: ellipsis`.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| MSA-01 | P1: modelo no card, sem prefixo | Execute | Verified |
| MSA-02 | P1: o que mais custou + "+N" | Execute | Verified |
| MSA-03 | P1: `title` com todos os modelos e custos | Execute | Verified |
| MSA-04 | P1: sem modelo, sem item | Execute | Verified |
| MSA-05 | P1: nome longo cortado, sem rolagem | Execute | Verified |

**Coverage:** 5 total, 5 mapeados para Execute, 0 sem mapeamento.
