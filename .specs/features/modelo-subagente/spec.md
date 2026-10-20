# modelo-subagente Specification

> Status: **implementada e verificada** em 2026-10-20. Três versões no mesmo dia: item "modelo" na linha do card (PR #22), item virou botão que abria a lista de modelos (PR #23) e, por fim, lista sempre visível com o custo, sem botão nem custo na linha (desenho do usuário no Figma, nó 46:466). Os critérios das duas primeiras ficam na rastreabilidade como substituídos. UAT em [`uat.md`](uat.md). Escopo pequeno: spec curta, design inline, sem `tasks.md`.

## Problem Statement

O card de cada subagente no pop-up mostrava vida, tokens e custo, mas não o modelo. Para decidir a orquestração (que subagente vale rodar num modelo mais barato ou mais forte) preciso ver o modelo ao lado do custo, sem somar de cabeça a tabela "Modelos", que junta todos os agentes.

## Goals

- [x] Do pop-up aberto, ver o modelo e o custo de cada modelo de cada subagente no próprio card, sem toque extra.

## Out of Scope

| Feature | Reason |
| --- | --- |
| Campo novo na API ou no `ingest` | `Agent.Models` já vem no `GET /api/session/{id}` desde o `custo-por-modelo` |
| Modelo no mini-tile da grade | O `AgentView` do SSE não traz `Models`; o tile segue enxuto |
| Modelo pedido na chamada do Agent (`model` do `tool_input`) | `tool_input` fica fora da lista branca (AD-003); o transcript diz o modelo que de fato respondeu |
| Nome amigável ("Haiku 5.5") | Exigiria uma tabela a manter junto com a de preços |

---

## User Stories

### P1: Modelos e custo no card do subagente ⭐ MVP

**User Story**: Como dono da máquina, quero ver no card de cada subagente os modelos que ele usou e quanto cada um custou, para decidir melhor a orquestração de modelos.

**Acceptance Criteria**:

1. WHEN o transcript do subagente já foi lido THEN o card SHALL mostrar, abaixo da linha de vida e tokens, uma lista com o nome completo de cada modelo e o custo dele, sem toque.
2. A lista SHALL vir do modelo mais caro para o mais barato, empate pelo nome (a ordem da tabela "Modelos").
3. O custo SHALL usar o formato do restante do pop-up (`n/d`, `≥ US$ X`).
4. A linha do card SHALL mostrar só vida, tokens e última (se houver): sem item "modelo" e sem item "custo".
5. WHEN o subagente ainda não tem modelo (`Models` vazio) THEN a lista SHALL NOT aparecer.
6. WHEN o nome é longo THEN ele SHALL quebrar linha dentro da lista, sem rolagem horizontal da página.
7. A seção "Tokens por agente" e a tabela "Modelos" do pop-up SHALL NOT mudar.

**Trade-off aceito:** com dois modelos ou mais, o card não mostra o custo total do subagente; a lista traz o de cada modelo (o desenho do usuário não tem linha Total).

**Independent Test**: Servidor com `-projects` apontando para transcripts sintéticos (um subagente por modelo, um com dois modelos, um com nome longo, um sem resposta ainda) e conferência do card em desktop e celular.

---

## Design (inline)

Tudo em `internal/web/static/index.html`; backend, API e hook não mudam. Referência: Figma, nó 46:466.

- `modelosDoAgente(a)` ordena `a.Models` pelo custo (como `montarModelos`).
- `modelosAgente(nome, lista)` monta a `ul.modelos-ag` (fundo `--surface-2`, raio 8, padding 6 × 8; nome em mono `--text`, custo em mono 500 `--muted` à direita), com `aria-label` "Custo por modelo de <subagente>". Sem estado: a lista sobrevive ao poll de 5 s sozinha.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| MSA-01 | v1: item "modelo" na linha, sem prefixo | Execute | Substituído por MSA-10 |
| MSA-02 | v1: o que mais custou + "+N" | Execute | Substituído por MSA-10 |
| MSA-03 | v1: nome completo e custo de cada modelo ao alcance | Execute | Substituído por MSA-10 |
| MSA-04 | v1: sem modelo, sem item | Execute | Substituído por MSA-12 |
| MSA-05 | v1: nome longo cortado | Execute | Substituído por MSA-13 |
| MSA-06 | v2: botão abre e fecha a lista | Execute | Substituído por MSA-10 |
| MSA-07 | v2: lista e foco sobrevivem ao poll | Execute | Substituído (lista sem estado) |
| MSA-08 | v2: `aria-*`, alvo ≥ `--toque-min` | Execute | Substituído (sem botão) |
| MSA-09 | v2: sem "custo" na linha do card | Execute | Incorporado em MSA-11 |
| MSA-10 | P1: lista sempre visível com nome completo e custo, mais caro primeiro, formato do pop-up | Execute | Verified |
| MSA-11 | P1: linha só com vida, tokens e última | Execute | Verified |
| MSA-12 | P1: sem modelo, sem lista | Execute | Verified |
| MSA-13 | P1: nome longo quebra, sem rolagem; "Tokens por agente" e "Modelos" iguais | Execute | Verified |

**Coverage:** 4 vigentes (MSA-10 a MSA-13), todos verificados; 9 substituídos.
