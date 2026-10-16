# ocultar-sessao Specification

> Status: **implementada e verificada** em 2026-10-16 (P1 e P2).

## Problem Statement

Gosto de olhar o painel com poucos tiles, os das sessões em que estou trabalhando. Meu assistente de voz, o Tony (sessão `toni-voz`), também aparece na grade, mas passa a maior parte do tempo parado, porque não falo com ele o tempo todo. O tile dele ocupa espaço e tira o foco. Quero poder ocultá-lo da grade principal sem perder o aviso quando ele precisar de mim.

## Goals

- [ ] Ocultar uma sessão custa 2 toques: abrir o pop-up e tocar em "Ocultar".
- [ ] Uma sessão nova com o mesmo nome (ex.: a próxima `toni-voz`) já nasce oculta, sem eu repetir nada.
- [ ] Nada que precise de mim fica escondido: sessão oculta em aguardando volta à grade e toca o alerta.

## Out of Scope

| Feature | Reason |
| --- | --- |
| Botão de ocultar no próprio tile | Decisão do usuário: o tile fica limpo e não se oculta nada sem querer; o botão só existe no pop-up |
| Lista de ocultos compartilhada entre aparelhos (servidor) | Decisão do usuário: guardada em cada navegador; celular e desktop têm listas separadas |
| Ocultar subagentes individualmente | O pedido é sobre a sessão inteira; os mini-tiles somem junto com o tile |
| Ocultar sessões por regra (padrão de nome, pasta, idade) | Um botão por sessão basta para o caso do Tony |

---

## User Stories

### P1: Ocultar uma sessão pelo pop-up ⭐ MVP

**User Story**: Como dono da máquina, quero ocultar da grade uma sessão que fica quase sempre parada, para ver só as sessões em que estou trabalhando.

**Why P1**: É o pedido.

**Acceptance Criteria**:

1. WHEN abro o pop-up de uma sessão visível THEN ele SHALL mostrar um botão "Ocultar da grade".
2. WHEN o tile está fechado (só na grade) THEN nenhum botão de ocultar SHALL aparecer nele.
3. WHEN toco em "Ocultar da grade" THEN o pop-up SHALL fechar, o tile SHALL sumir da grade e a escolha SHALL ficar salva no navegador.
4. WHEN a sessão tem nome (`--name` ou `/rename`) THEN a ocultação SHALL valer para esse nome: sessões futuras com o mesmo nome já nascem ocultas.
5. WHEN a sessão não tem nome THEN a ocultação SHALL valer para o `cwd` dela.
6. WHEN há sessões ocultas THEN o contador do cabeçalho SHALL mostrar um chip "N oculta(s)", e o chip "sessões" SHALL contar só as visíveis.

**Independent Test**: Abrir o pop-up do `toni-voz`, ocultar, ver o tile sumir e o chip "1 oculta"; recarregar a página e confirmar que segue oculto; reiniciar a sessão do Tony com o mesmo nome e confirmar que ela já nasce oculta.

---

### P1: Sessão oculta que precisa de mim reaparece ⭐ MVP

**User Story**: Como dono da máquina, quero que uma sessão oculta volte a aparecer e toque o alerta quando pedir permissão, para não deixar o Tony travado sem perceber.

**Why P1**: Decisão do usuário: ocultar não pode virar silenciar.

**Acceptance Criteria**:

1. WHILE uma sessão oculta, ou um subagente dela, está em **aguardando** THEN o tile SHALL aparecer na grade, com o mesmo destaque de qualquer sessão aguardando.
2. WHEN ela entra em aguardando THEN o alerta sonoro (`alerta-sonoro`) SHALL tocar normalmente, se estiver ligado.
3. WHEN ela sai de aguardando THEN o tile SHALL voltar a ficar oculto, sem eu fazer nada.
4. O chip "aguardando" do cabeçalho SHALL contar sessões ocultas em aguardando.

**Independent Test**: Com o `toni-voz` oculto, mandar um `PermissionRequest` para ele via `curl` e ver o tile voltar e o som tocar; mandar `PostToolUse` e ver o tile sumir de novo.

---

### P2: Ver e reexibir sessões ocultas

**User Story**: Como dono da máquina, quero ver as sessões ocultas e desfazer a ocultação, para não depender de limpar o navegador.

**Why P2**: Sem isso não há como desfazer, mas o caso do Tony funciona sem.

**Acceptance Criteria**:

1. WHEN toco no chip "N oculta(s)" THEN a grade SHALL alternar entre esconder e mostrar as ocultas; as ocultas aparecem esmaecidas e marcadas como ocultas.
2. WHEN abro o pop-up de uma sessão oculta THEN o botão SHALL dizer "Mostrar na grade" e, ao tocar, desfazer a ocultação daquele nome (ou `cwd`).
3. WHEN nenhuma sessão oculta está aberta THEN o chip SHALL sumir (os nomes ocultos continuam salvos, valendo para sessões futuras).

**Independent Test**: Tocar em "1 oculta", ver o tile esmaecido do Tony, abrir o pop-up, tocar em "Mostrar na grade" e ver o tile voltar ao normal.

---

## Edge Cases

- WHEN a sessão é renomeada (`/rename`) depois de oculta THEN ela SHALL voltar a aparecer, porque a regra vale para o nome antigo; ocultar de novo grava o nome novo.
- WHEN duas sessões abertas têm o mesmo nome ou `cwd` THEN ocultar uma SHALL ocultar as duas (a regra é pelo nome, não pelo ID); o pop-up SHALL deixar isso claro no rótulo, ex.: "Ocultar sessões 'toni-voz'".
- WHEN o armazenamento do navegador não está disponível (aba anônima, bloqueio) THEN ocultar SHALL valer só enquanto a página estiver aberta, sem erro.
- WHEN a sessão oculta é a que está com o pop-up aberto por foco de teclado THEN, ao fechar, o foco SHALL ir para o primeiro tile visível.

---

## Design (como foi implementado)

Tudo em `internal/web/static/index.html`; backend, API e hook não mudam.

- **Chave**: `nome:<Name>` quando `Name` existe; senão `cwd:<Cwd>`; sem os dois, `id:<ID>`. `SessionView` e `SessionDetail` já trazem os campos.
- **Armazenamento**: `localStorage` `fleet.ocultas`, um array JSON de chaves, com toda leitura e escrita em `try/catch` (mesmo padrão de `fleet.som`).
- **Filtro**: `render()` separa visíveis e ocultas; uma oculta entra na grade se `precisaAtencao(s)` (sessão ou subagente aguardando) ou se o modo "mostrar ocultas" está ligado.
- **Botão**: no `.det-topo` do pop-up, ao lado do botão de fechar: "Ocultar" (olho cortado) ou "Mostrar na grade" (olho), com o rótulo acessível dizendo o que é afetado, ex.: `Ocultar da grade as sessões "toni-voz"`.
- **Tile oculto exibido pelo chip**: classe `oculta` (esmaecido, borda tracejada) e ícone de olho cortado no canto. Oculta que volta por estar aguardando aparece sem essa marca.
- **Com tudo oculto**: a grade mostra "Todas as sessões estão ocultas."
- **Alerta sonoro**: `avaliarSom` continua recebendo todas as sessões, sem filtro, então SOM-01 e SOM-02 valem para as ocultas sem mudança.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| OCU-01 | P1: Ocultar — botão só no pop-up, nunca no tile | Execute | Verified |
| OCU-02 | P1: Ocultar — chave por nome, com `cwd` de reserva; vale para sessões futuras | Execute | Verified |
| OCU-03 | P1: Ocultar — salvo no navegador, tolera armazenamento indisponível | Execute | Verified |
| OCU-04 | P1: Ocultar — chips "sessões" e "N oculta(s)" no cabeçalho | Execute | Verified |
| OCU-05 | P1: Reaparece — oculta em aguardando volta à grade e alerta | Execute | Verified |
| OCU-06 | P2: Reexibir — chip alterna mostrar ocultas; pop-up desfaz a ocultação | Execute | Verified |

**Coverage:** 6 total, 6 mapeados para Execute, 0 sem mapeamento.

---

## Success Criteria

- [ ] Com o `toni-voz` oculto, a grade mostra só as sessões de trabalho, no celular e no desktop (cada um com a sua escolha).
- [ ] Um `PermissionRequest` no `toni-voz` oculto faz o tile aparecer e o som tocar em ≤ 2 s.
