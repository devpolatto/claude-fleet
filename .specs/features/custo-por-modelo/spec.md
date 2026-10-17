# custo-por-modelo Specification

> Origem: [issue #3](https://github.com/devpolatto/claude-fleet/issues/3), aprovada pelo mantenedor em 2026-10-16. Escopo médio: spec curta, design inline, sem `tasks.md`.

## Problem Statement

O pop-up mostra o total de tokens e o custo estimado da sessão, mas não diz **onde** o custo está. A seção "Modelos" lista só os nomes, sem tokens nem custo de cada um, e os contadores de cache aparecem soltos, sem uma taxa que mostre se a sessão reaproveita o contexto ou reescreve o cache toda hora.

## Goals

- [ ] Do pop-up aberto, ver qual modelo pesa mais no custo da sessão, somando sessão principal e subagentes, sem toque extra.
- [ ] Ver a taxa de acerto de cache de cada modelo e da sessão inteira.

## Out of Scope

| Feature | Reason |
| --- | --- |
| Campo novo na API ou no `ingest` | `Session.Models` e `Agent.Models` já trazem `Tokens`, `CostUSD` e `CostKnown` por modelo |
| Histórico ou custo por período | Ideia diferida "Histórico em SQLite" |
| Mudar a tabela de preços | Fica no todo "Revisar a tabela de preços" |
| Custo por modelo no tile da grade | O tile segue com o total; o detalhe fica no pop-up |

---

## User Stories

### P1: Custo e tokens por modelo no pop-up ⭐ MVP

**User Story**: Como dono da máquina, quero ver quanto cada modelo custou numa sessão, para saber se vale trocar o modelo de um subagente.

**Acceptance Criteria**:

1. WHEN o pop-up abre THEN a seção "Modelos" SHALL mostrar uma linha por modelo com tokens (soma das 5 categorias), custo estimado e acerto de cache.
2. WHEN o mesmo modelo aparece na sessão principal e em subagentes THEN a linha SHALL somar `d.Models` com o `Models` de cada subagente.
3. WHEN há dois modelos ou mais THEN a seção SHALL ter uma linha Total, com o `Total` da sessão.
4. WHEN um modelo não tem preço (`CostKnown` falso) THEN o custo SHALL usar o mesmo formato do restante do pop-up (`n/d`, ou `≥ US$ X` na soma parcial), nunca `US$ 0`.
5. WHEN o custo por modelo aparece THEN a seção SHALL manter o rótulo de estimativa (AD-005).
6. As linhas SHALL vir do modelo mais caro para o mais barato; empate pelo nome.

### P1: Taxa de acerto de cache ⭐ MVP

**Acceptance Criteria**:

1. A taxa SHALL ser `CacheRead / (Input + Cache5m + Cache1h + CacheRead)`, em porcentagem com até uma casa.
2. WHEN o denominador é zero THEN a célula SHALL mostrar "—".

**Independent Test**: Servidor com `-projects` apontando para transcripts sintéticos (dois modelos com preço, um sem, o mesmo modelo na principal e num subagente) e conferência dos valores contra a conta feita à mão.

---

## Edge Cases

- WHEN a sessão ainda não teve o transcript lido (sem `Models`) THEN a seção SHALL mostrar "—", como hoje.
- WHEN o uso de um subagente está zerado THEN ele conta como custo conhecido de US$ 0, igual ao Store (`costKnown`).
- WHEN a tela tem 360 px THEN a tabela SHALL caber sem rolagem horizontal da página; nomes longos de modelo são cortados com reticências e o nome inteiro fica no `title`.

---

## Design (inline)

Tudo em `internal/web/static/index.html`; backend, API e hook não mudam.

- `usoPorModelo(d, subs)` soma os mapas `Models` com `somarUso`, que replica o `Usage.add` do Store (custo conhecido só se os dois forem, uso zerado conta como conhecido).
- `montarModelos` reaproveita a tabela `.tokens` da seção "Tokens por agente" (mesmas bordas, linha `soma` em negrito, células `zero` esmaecidas) com quatro colunas: Modelo, Tokens, Custo, Cache. Uma legenda abaixo traz o rótulo de estimativa e a fórmula do cache.
- O custo usa `formatarUso`, o mesmo do restante do pop-up. Os chips `.modelos`/`.modelo` saem do CSS.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| CPM-01 | P1: Uma linha por modelo com tokens, custo e cache | Execute | Verified |
| CPM-02 | P1: Soma da principal com os subagentes | Execute | Verified |
| CPM-03 | P1: Linha Total com dois modelos ou mais | Execute | Verified |
| CPM-04 | P1: Modelo sem preço não vira US$ 0 | Execute | Verified |
| CPM-05 | P1: Rótulo de estimativa junto do custo | Execute | Verified |
| CPM-06 | P1: Taxa de cache com "—" no denominador zero | Execute | Verified |

**Coverage:** 6 total, 6 mapeados para Execute, 0 sem mapeamento.

---

## Success Criteria

- [ ] Valores da seção "Modelos" iguais à conta feita à mão sobre o transcript sintético (ver `uat.md`).
- [ ] Sem rolagem horizontal no desktop, no celular em pé e deitado.
