# alerta-fim Specification

Issue: [#15](https://github.com/devpolatto/claude-fleet/issues/15). Estende o [`alerta-sonoro`](../alerta-sonoro/spec.md).

## Problem Statement

Quando o agente principal termina o turno, o tile só muda para **parado**. Longe da tela, ou com o painel numa aba atrás de outras, não percebo que a tarefa acabou e que a sessão espera o próximo prompt. Quero um aviso sonoro de fim, distinto do de aguardando, que não bipe a cada resposta curta enquanto estou no terminal.

## Goals

- [ ] Com o som ligado, o fim de um turno longo da sessão principal toca um alerta próprio uma vez, em ≤ 2 s.
- [ ] Turnos curtos e subagentes não fazem barulho.

## Out of Scope

| Feature | Reason |
| --- | --- |
| Push "terminou" (`internal/push/notifier.go`) | Mexe no backend; fica como ideia diferida no `STATE.md` |
| Toggle separado para o som de fim | Só se o alerta único incomodar na prática; exigiria mudança visual e Figma |
| Lembrete repetido do fim | Sessão parada não trava nada |
| Limite configurável pela UI | Valor fixo basta para o uso pessoal |

---

## User Stories

### P1: Alerta sonoro de fim de turno ⭐ MVP

**User Story**: Como dono da máquina, quero ouvir um aviso quando a sessão principal terminar uma tarefa demorada, para voltar ao terminal e mandar o próximo prompt.

**Why P1**: É o motivo da feature.

**Acceptance Criteria**:

1. WHEN o som está ligado e uma sessão passa de **ativo** para **parado** depois de um turno de pelo menos 20 s THEN a página SHALL tocar o alerta de fim uma vez, mesmo que várias terminem no mesmo snapshot.
2. WHEN o turno durou menos de 20 s THEN a página SHALL NOT tocar.
3. WHEN um subagente termina (`SubagentStop`) THEN a página SHALL NOT tocar o alerta de fim.
4. WHEN a sessão chega a parado vindo de outro estado que não ativo (nasce parada no `SessionStart`, sai de `desconhecido` no boot, ou sai de aguardando por permissão recusada, L-002) THEN a página SHALL NOT tocar.
5. WHEN a página carrega ou reconecta THEN o primeiro snapshot SHALL ser só a linha de base: nada toca.
6. WHEN o som está desligado THEN a página SHALL NOT tocar nada.
7. O alerta de fim SHALL soar diferente do de aguardando: duas notas descendentes e mais suaves, contra os dois bipes subindo.

**Independent Test**: Com o som ligado, mandar via `curl` um `UserPromptSubmit`, esperar 20 s e mandar `Stop`: ouvir as duas notas descendentes. Repetir com 5 s entre os eventos: silêncio.

---

## Edge Cases

- WHEN o turno passa por aguardando (`ativo → aguardando → ativo → parado`) THEN a duração SHALL contar desde a entrada em ativo, incluindo a espera pela permissão.
- WHEN a página carrega no meio de um turno THEN o início SHALL ser o instante da linha de base (o snapshot não diz desde quando a sessão está ativa). A duração fica subestimada: na dúvida, não toca.
- WHEN a conexão cai e volta THEN o início de toda sessão em turno SHALL recomeçar na nova linha de base, mesmo que já fosse conhecido: durante a queda o turno pode ter acabado e outro começado (no celular, o SSE cai a cada bloqueio de tela). Um turno longo que atravessa a queda só avisa se durar ≥ 20 s depois dela.
- O Store junta notificações para cliente lento (buffer de 1). Se um `Stop` seguido logo de um `UserPromptSubmit` cair no mesmo snapshot, a página vê ativo → ativo e o turno novo herda o início do anterior; um turno curto logo depois pode tocar. Limitação aceita: só acontece com a escrita para o cliente travada, e corrigir exigiria um campo novo no snapshot.
- WHEN a sessão é encerrada (`SessionEnd`) ou some da grade THEN SHALL NOT tocar, e o início guardado SHALL ser descartado.
- WHEN o mesmo snapshot traz uma entrada em aguardando e um fim de turno THEN SHALL tocar só o alerta de aguardando, que é o mais urgente.

---

## Design (inline)

Tudo em `internal/web/static/index.html`; backend, API, hook e visual não mudam.

- **Turno**: a sessão está "em turno" com status `ativo` ou `aguardando`. Um `Map` guarda, por `session_id`, o instante em que ela entrou em turno vindo de fora dele; sessão fora de turno sai do mapa.
- **Detecção**: a cada snapshot, compara o status de cada sessão com o do snapshot anterior. `ativo` → `parado` com `Date.now() - início ≥ 20 s` marca um fim. Qualquer fim no snapshot toca uma vez.
- **Linha de base**: no primeiro snapshot da conexão grava os status, regrava com o instante atual o início de toda sessão em turno e não toca.
- **Som**: `tocarFim()` com dois osciladores senoidais, ~660 Hz e depois ~440 Hz, ~0,25 s cada, ganho de pico 0,18 (o de aguardando usa 0,3). Mesmo `AudioContext` e o mesmo toggle (`fleet.som`).
- **Lembrete**: o fim não mexe na `referencia` do lembrete de aguardando.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| FIM-01 | P1: toca uma vez em ativo → parado após ≥ 20 s | Execute | Verified |
| FIM-02 | P1: turno curto não toca | Execute | Verified |
| FIM-03 | P1: subagente não toca | Execute | Verified |
| FIM-04 | P1: parado vindo de outro estado não toca | Execute | Verified |
| FIM-05 | P1: primeiro snapshot é linha de base | Execute | Verified |
| FIM-06 | P1: toggle desligado silencia | Execute | Verified |
| FIM-07 | P1: som distinto do de aguardando | Execute | Verified |

**Coverage:** 7 total, 7 mapeados para Execute, 0 sem mapeamento.

---

## Success Criteria

- [ ] Um turno simulado de ≥ 20 s toca o alerta de fim no desktop com o painel aberto e o som ligado.
- [ ] Nenhum som de fim com o toggle desligado, em turnos curtos ou no fim de subagentes.
