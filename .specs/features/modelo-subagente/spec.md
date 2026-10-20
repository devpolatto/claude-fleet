# modelo-subagente Specification

> Status: **implementada e verificada** em 2026-10-20 (PR #22); lista aberta por toque acrescentada no mesmo dia (MSA-06 a MSA-08, PR #23); custo só na lista do modelo (MSA-09). UAT em [`uat.md`](uat.md). Escopo pequeno: spec curta, design inline, sem `tasks.md`.

## Problem Statement

O card de cada subagente no pop-up mostrava vida, tokens e custo, mas não o modelo. Para decidir a orquestração (que subagente vale rodar num modelo mais barato ou mais forte) preciso ver o modelo ao lado do custo, sem somar de cabeça a tabela "Modelos", que junta todos os agentes.

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

1. WHEN o transcript do subagente já foi lido THEN o card SHALL mostrar "modelo <nome>" antes dos outros itens da linha, com o nome sem o prefixo `claude-`.
2. WHEN o subagente usou mais de um modelo THEN o card SHALL mostrar o que mais custou seguido de "+N"; empate pelo nome.
3. O nome completo e o custo de cada modelo SHALL estar ao alcance de um toque (ver a próxima história), no formato do restante do pop-up (`n/d`, `≥ US$ X`). A primeira versão usava `title`, que não abre no celular.
4. WHEN o subagente ainda não tem modelo (`Models` vazio) THEN o item SHALL NOT aparecer.
5. WHEN o nome é longo demais para a largura THEN ele SHALL ser cortado com reticências, sem rolagem horizontal da página.

**Independent Test**: Servidor com `-projects` apontando para transcripts sintéticos (um subagente por modelo, um com dois modelos, um sem resposta ainda) e conferência do card em desktop e celular.

### P1: Lista de modelos com um toque

**User Story**: Como dono da máquina, quero tocar no modelo do card para ver o nome inteiro e o custo de cada modelo, porque no celular o `title` não abre.

**Acceptance Criteria**:

1. O item "modelo" SHALL ser um botão com seta; um toque (ou Enter/Espaço) abre, logo abaixo da linha do card, uma lista com o nome completo e o custo de cada modelo, na ordem do item; outro toque fecha.
2. WHILE o pop-up está aberto THEN a lista SHALL continuar aberta (e o foco no botão) a cada atualização de 5 s; fechar o pop-up fecha todas.
3. O botão SHALL ter `aria-expanded` e `aria-controls` e alvo de toque com pelo menos `--toque-min` de altura; nome longo na lista quebra linha, sem rolagem horizontal.

### P2: Custo só na lista do modelo

**User Story**: Como dono da máquina, quero ver o custo do subagente num lugar só, ao lado do modelo, para o card ficar mais limpo.

**Acceptance Criteria**:

1. A linha do card SHALL mostrar modelo, vida, tokens (e última, se houver), sem o item "custo"; o custo de cada modelo fica só na lista aberta pelo botão do modelo.
2. A seção "Tokens por agente" e a tabela "Modelos" do pop-up SHALL NOT mudar.

**Trade-off aceito:** com dois modelos ou mais, o card não mostra o custo total do subagente; a lista traz o de cada modelo.

---

## Design (inline)

Tudo em `internal/web/static/index.html`; backend, API e hook não mudam.

- `modelosDoAgente(a)` ordena `a.Models` pelo custo (como `montarModelos`).
- `botaoModelo` monta o `button.modelo-btn` (tipografia da linha `.meta`, `min-height: var(--toque-min)` com margem negativa de `(1lh − --toque-min) / 2` e `z-index` acima da linha seguinte, nome cortado com reticências, ícone `seta` que gira 180° aberto) e `modelosAgente` monta a `ul.modelos-ag` com fundo `--surface-2`.
- Estado em `det.modelosAbertos` (IDs dos subagentes), limpo em `openDetail`. O clique entra no mesmo listener delegado do `.silenciar-sub`; o foco volta pelo `data-foco` comum aos dois botões.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| MSA-01 | P1: modelo no card, sem prefixo | Execute | Verified |
| MSA-02 | P1: o que mais custou + "+N" | Execute | Verified |
| MSA-03 | P1: nome completo e custo de cada modelo a um toque (o `title` da primeira versão saiu; ver MSA-06) | Execute | Verified |
| MSA-04 | P1: sem modelo, sem item | Execute | Verified |
| MSA-05 | P1: nome longo cortado, sem rolagem | Execute | Verified |
| MSA-06 | P1: botão abre e fecha a lista com nome e custo | Execute | Verified |
| MSA-07 | P1: lista e foco sobrevivem ao poll; fechar o pop-up fecha | Execute | Verified |
| MSA-08 | P1: `aria-*`, alvo ≥ `--toque-min`, nome longo quebra | Execute | Verified |
| MSA-09 | P2: sem "custo" na linha do card; custo só na lista | Execute | Verified |

**Coverage:** 9 total, 9 mapeados para Execute, 0 sem mapeamento.
