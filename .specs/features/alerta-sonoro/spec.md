# alerta-sonoro Specification

## Problem Statement

Quando uma sessão ou um subagente para pedindo permissão, o painel só muda de cor. Longe da tela, ou com o painel numa aba atrás de outras, eu não percebo e o trabalho fica parado esperando. Quero um aviso sonoro no navegador, fácil de ligar e desligar na própria página.

## Goals

- [ ] Com o som ligado, toda entrada de uma sessão ou subagente em **aguardando** toca um alerta em ≤ 2 s (o mesmo prazo do tile).
- [ ] Ligar ou desligar o som custa 1 toque, e a escolha sobrevive a recarregar a página.

## Out of Scope

| Feature | Reason |
| --- | --- |
| Vibração e notificação do sistema (Notification API) | Primeiro passo é só o som; ficam no item do M2 |
| Som com a tela do celular bloqueada ou o navegador fechado | Exige PWA + push; o navegador suspende a página |
| Substituir o `telegram-notify` | Avaliado depois, junto com notificação do sistema |
| Escolher o som, volume ou intervalo pela UI | Um som fixo basta para o uso pessoal |
| Arquivo de áudio servido pelo backend | O som é gerado no navegador (Web Audio); o binário não muda |

---

## User Stories

### P1: Alerta sonoro ao entrar em aguardando ⭐ MVP

**User Story**: Como dono da máquina, quero ouvir um aviso quando um agente parar pedindo permissão, para não deixar trabalho travado sem perceber.

**Why P1**: É o motivo da feature.

**Acceptance Criteria**:

1. WHEN o som está ligado e uma sessão ou um subagente passa a **aguardando** (não estava assim no snapshot anterior) THEN a página SHALL tocar o alerta uma vez, mesmo que vários entrem no mesmo snapshot.
2. WHILE o som está ligado e há ao menos uma sessão ou subagente em aguardando THEN a página SHALL repetir o alerta a cada 30 s, contados do último toque.
3. WHEN nada mais está em aguardando THEN o lembrete SHALL parar.
4. WHEN a página carrega ou reconecta e já há algo em aguardando THEN ela SHALL NOT tocar na hora (o primeiro snapshot é a linha de base); o lembrete de 30 s cobre o caso.
5. WHEN o som está desligado THEN a página SHALL NOT tocar nada.

**Independent Test**: Com o som ligado, mandar um `PermissionRequest` via `curl` para uma sessão e ouvir o alerta; esperar 30 s e ouvir de novo; mandar `PostToolUse` e confirmar que o lembrete parou.

---

### P1: Toggle de som no cabeçalho ⭐ MVP

**User Story**: Como dono da máquina, quero um botão no cabeçalho para ligar e desligar o som, para silenciar o painel quando não preciso dele.

**Why P1**: Pedido explícito; e o navegador só libera áudio depois de um gesto na página, que o toggle fornece.

**Acceptance Criteria**:

1. WHEN abro a página pela primeira vez THEN o som SHALL estar desligado.
2. WHEN toco no toggle THEN o estado SHALL alternar, o ícone e o rótulo acessível (`aria-pressed`) SHALL refletir o novo estado, e a escolha SHALL ficar salva no navegador.
3. WHEN ligo o som THEN a página SHALL tocar o alerta uma vez, como confirmação (e para destravar o áudio).
4. WHEN recarrego a página com o som salvo como ligado e o navegador ainda bloqueia o áudio THEN o toggle SHALL indicar "toque na página para liberar o som", e o primeiro toque em qualquer ponto SHALL liberar o áudio sem mudar o estado.
5. WHEN o armazenamento do navegador não está disponível (aba anônima, bloqueio) THEN o toggle SHALL funcionar só na sessão da página, sem erro.

**Independent Test**: Abrir o painel, ver o toggle desligado; ligar e ouvir a confirmação; recarregar e ver o estado mantido.

---

## Edge Cases

- WHEN uma sessão sai de aguardando e volta antes do próximo snapshot THEN conta como uma entrada só se o snapshot a mostrar fora de aguardando entre as duas; o Store manda um snapshot por mudança, então isso é o caso normal.
- WHEN uma sessão em aguardando some da grade (encerrada e removida) THEN ela SHALL sair do conjunto de aguardando e não tocar mais.
- WHEN a aba está em segundo plano no desktop THEN o som SHALL tocar mesmo assim (o SSE e os timers seguem vivos; o navegador pode atrasar o lembrete em alguns segundos).
- WHEN o navegador não tem Web Audio THEN o toggle SHALL ficar oculto.

---

## Design (inline)

Tudo em `internal/web/static/index.html`; backend, API e hook não mudam.

- **Som**: Web Audio API, dois bipes curtos (osciladores senoidais, ~880 Hz e ~1320 Hz, ~0,15 s cada, com envelope para não estalar). Um `AudioContext` único, criado no primeiro gesto do usuário.
- **Detecção**: a cada snapshot, monta o conjunto de chaves em aguardando (`<session_id>` para sessão, `<session_id>/<agent_id>` para subagente). Chave nova que não estava no conjunto anterior → toca. O primeiro snapshot de cada conexão só grava a linha de base.
- **Lembrete**: um `setInterval` de 5 s verifica se há algo em aguardando e se já passaram 30 s do último toque.
- **Toggle**: botão no cabeçalho, ao lado do indicador "ao vivo", com ícone de alto-falante ligado/cortado. Estado em `localStorage` (`fleet.som`), com toda leitura e escrita em `try/catch`.
- **Bloqueio do navegador**: se o `AudioContext` está `suspended`, o toggle ganha a classe `bloqueado` e o título explica; um listener de `pointerdown`/`keydown` no documento chama `resume()`.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| SOM-01 | P1: Alerta — toca ao entrar em aguardando (sessão ou subagente) | Execute | Verified |
| SOM-02 | P1: Alerta — lembrete a cada 30 s enquanto houver aguardando | Execute | Verified |
| SOM-03 | P1: Alerta — primeiro snapshot é linha de base | Execute | Verified |
| SOM-04 | P1: Toggle — desligado por padrão, persistido no navegador | Execute | Verified |
| SOM-05 | P1: Toggle — confirmação sonora ao ligar | Execute | Verified |
| SOM-06 | P1: Toggle — indica e destrava áudio bloqueado | Execute | Verified |
| SOM-07 | P1: Toggle — funciona sem armazenamento e some sem Web Audio | Execute | Verified |

**Coverage:** 7 total, 7 mapeados para Execute, 0 sem mapeamento.

---

## Success Criteria

- [ ] No celular (via Tailscale) e no desktop, um `PermissionRequest` simulado toca o alerta com o painel aberto e o som ligado.
- [ ] Nenhum som com o toggle desligado, em nenhuma sequência de eventos.
