# silenciar-agente Specification

Issue: [#19](https://github.com/devpolatto/claude-fleet/issues/19). Estende o [`alerta-sonoro`](../alerta-sonoro/spec.md).

## Problem Statement

Com três agentes em paralelo esperando permissão, o lembrete toca a cada 30 s enquanto houver qualquer um em aguardando. Às vezes não dá para aprovar na hora porque preciso checar algo antes, mas o bipe não para. Hoje a única saída é desligar o som global, e aí perco o aviso dos outros agentes, que é justamente o que quero manter.

## Goals

- [ ] Calar o alerta de um agente em aguardando custa 2 toques (abrir o pop-up, tocar em "Silenciar") e não cala os outros.
- [ ] Nenhum agente fica mudo para sempre: o silêncio vale só para a espera em curso.

## Out of Scope

| Feature | Reason |
| --- | --- |
| Silenciar o push de aguardando (`internal/push/notifier.go`) | O push é do servidor e compartilhado entre aparelhos; exigiria estado no servidor e uma rota de escrita. Fica como ideia diferida |
| Silêncio persistente (até desfazer) | Decisão do usuário: fácil esquecer um agente mudo e perder o próximo pedido |
| Silêncio com prazo (5 / 15 min) | Acrescenta UI; ideia diferida |
| Guardar os silenciados no `localStorage` | Decisão do usuário: só em memória. O silêncio dura uma espera; recarregar a página o desfaz |
| Silenciar a sessão leva junto os subagentes | Decisão do usuário: chaves independentes |
| Silenciar o alerta de fim de turno (`alerta-fim`) | Quando o turno acaba a espera já terminou e o silêncio já caiu |

---

## User Stories

### P1: Silenciar um agente em aguardando ⭐ MVP

**User Story**: Como dono da máquina, quero calar o alerta de um agente que está esperando permissão, para checar algo com calma sem perder o aviso dos outros.

**Why P1**: É o pedido.

**Acceptance Criteria**:

1. WHEN a sessão está em aguardando THEN o pop-up dela SHALL mostrar um botão "Silenciar" ao lado de "Ocultar"; fora de aguardando (e sem silêncio ativo) o botão SHALL NOT aparecer.
2. WHEN um subagente está em aguardando THEN a linha dele na lista de subagentes do pop-up SHALL mostrar o mesmo controle.
3. WHEN silencio um agente THEN ele SHALL NOT disparar o bipe nem contar para o lembrete de 30 s; os outros agentes em aguardando SHALL continuar tocando.
4. WHEN todos os agentes em aguardando estão silenciados THEN o lembrete SHALL NOT tocar.
5. Sessão e subagente SHALL ter chaves independentes (`"<sessão>"` e `"<sessão>/<subagente>"`): silenciar um não silencia o outro.
6. WHEN toco de novo no mesmo controle THEN o silêncio SHALL ser desfeito e o agente volta a contar para o lembrete.
7. O tile SHALL continuar em aguardando, com o destaque normal; só o som muda.
8. WHEN o som global está desligado THEN nada SHALL tocar; ligado, toca tudo menos os silenciados.

**Independent Test**: Com o som ligado, mandar `PermissionRequest` para duas sessões via `curl`, silenciar uma no pop-up e ouvir o lembrete seguir tocando pela outra; silenciar a outra e ver o lembrete parar.

---

### P1: O silêncio vale só para esta espera ⭐ MVP

**User Story**: Como dono da máquina, quero que o silêncio caia sozinho quando o agente sai de aguardando, para não esquecer um agente mudo.

**Why P1**: Decisão do usuário.

**Acceptance Criteria**:

1. WHEN o agente silenciado sai de aguardando (aprovei, negou, terminou, sumiu) THEN o silêncio SHALL ser desfeito no mesmo snapshot.
2. WHEN ele volta a entrar em aguardando THEN a página SHALL tocar normalmente.
3. WHEN a página é recarregada THEN nenhum agente SHALL estar silenciado.

**Independent Test**: Silenciar a sessão A, mandar `PostToolUse` para A, ver a marca sumir; mandar `PermissionRequest` de novo e ouvir o bipe.

---

### P1: O estado silenciado é visível

**User Story**: Como dono da máquina, quero ver na grade quem está silenciado, para não confundir silêncio com falta de pedido.

**Why P1**: Sem isso o silêncio fica invisível fora do pop-up.

**Acceptance Criteria**:

1. WHILE a sessão está silenciada THEN o tile SHALL mostrar um ícone de alto-falante cortado e o `aria-label` SHALL incluir ", alerta silenciado".
2. WHILE um subagente está silenciado THEN o mini-tile dele SHALL mostrar o mesmo ícone, pequeno.
3. Os controles do pop-up SHALL ter `aria-pressed` refletindo o estado e `aria-label` com o nome do agente.

---

## Edge Cases

- WHEN a conexão SSE cai e volta THEN o primeiro snapshot é linha de base (nada toca); o silêncio de quem continua em aguardando SHALL ser mantido, e o de quem saiu, desfeito.
- O pop-up decide a visibilidade do controle pelo último snapshot do SSE, não pelo poll de 5 s do detalhe, para bater com o que toca.
- WHEN a sessão silenciada é ocultada (`ocultar-sessao`) THEN ela volta à grade por estar em aguardando, como hoje, e mostra a marca de silenciada.
- Permissão recusada no modo interativo não dispara hook (L-002): o agente pode ficar em aguardando até o próximo evento, e o silêncio dura junto. É o comportamento desejado.

---

## Design (inline)

Tudo em `internal/web/static/index.html`; backend, API e hook não mudam.

- **Estado**: `silenciados`, um `Set` em memória com as mesmas chaves de `chavesAguardando()`.
- **Poda**: a cada snapshot, antes de desenhar a grade, remove do conjunto as chaves que não estão mais em aguardando.
- **Bipe**: `avaliarSom` ignora chave nova silenciada. **Lembrete**: toca só se há chave em aguardando fora de `silenciados`.
- **Controles**: botão no `.det-acoes` do pop-up (estilo do "Ocultar") e um botão pequeno na linha de cada subagente em aguardando. Alternar redesenha a grade e o pop-up; o pop-up não fecha.
- **Marca**: ícone `somDesligado` no tile e no mini-tile.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| SIL-01 | P1: controle no pop-up da sessão em aguardando | Execute | Pending |
| SIL-02 | P1: controle por subagente em aguardando | Execute | Pending |
| SIL-03 | P1: silenciado não bipa nem conta no lembrete; os outros tocam | Execute | Pending |
| SIL-04 | P1: todos silenciados, lembrete para | Execute | Pending |
| SIL-05 | P1: chaves independentes | Execute | Pending |
| SIL-06 | P1: desfazer pelo mesmo controle | Execute | Pending |
| SIL-07 | P1: som global manda | Execute | Pending |
| SIL-08 | P1: silêncio cai ao sair de aguardando | Execute | Pending |
| SIL-09 | P1: recarregar desfaz | Execute | Pending |
| SIL-10 | P1: marca no tile e no mini-tile, `aria-*` | Execute | Pending |

**Coverage:** 10 total, 10 mapeados para Execute, 0 sem mapeamento.

---

## Success Criteria

- [ ] Com três agentes em aguardando, silenciar um mantém o lembrete pelos outros dois; silenciar os três o cala.
- [ ] Layout conferido no desktop e no celular (vertical e horizontal); Figma atualizado.
