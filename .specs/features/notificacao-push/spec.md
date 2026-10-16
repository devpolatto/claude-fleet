# notificacao-push Specification

> Status: **P1 implementada e verificada** em 2026-10-16: notificação de teste, entrada em aguardando com a tela bloqueada e lembrete de 5 min chegaram no Android. P2 cancelada (o Telegram fica).

## Problem Statement

O alerta sonoro (`alerta-sonoro`) só toca com o painel aberto e a tela ligada. Com o celular bloqueado ou o app fechado, uma sessão parada pedindo permissão passa despercebida. Quero o aviso pelo próprio Fleet, como notificação do sistema, mesmo com a tela bloqueada.

## Goals

- [ ] Com as notificações ligadas num aparelho, toda entrada de uma sessão ou subagente em **aguardando** gera uma notificação nele em ≤ 5 s, com o app fechado e a tela bloqueada.
- [ ] Enquanto a sessão seguir aguardando, a notificação se repete a cada 5 min.
- [ ] O binário segue sem dependências externas: o Web Push (VAPID, RFC 8292, e criptografia, RFC 8291) é feito com a biblioteca padrão do Go.
- [x] ~~Depois de validado, desligar o modo `permission` do `telegram-notify`~~ — cancelado em 2026-10-16: esse modo não é só aviso, ele permite **aprovar ou negar pelo Telegram** (botões, espera até 1 h) e só age no "modo ausente". O push avisa; o Telegram segue para aprovar de longe.

## Out of Scope

| Feature | Reason |
| --- | --- |
| Conteúdo do trabalho na notificação (descrição do subagente, ferramenta, comando) | Aparece na tela bloqueada; a notificação diz só qual sessão e que ela aguarda |
| Aprovar a permissão pela notificação | O painel é somente leitura (fleet-mvp) |
| Respeitar a lista de ocultas | A lista vive em cada navegador; e sessão oculta que aguarda já deve alertar (ocultar-sessao) |
| Silenciar o push quando o painel está aberto no aparelho | O Chrome exige mostrar uma notificação a cada push; a `tag` por sessão evita empilhar |
| Mexer no `telegram-notify` | O modo `permission` aprova à distância, coisa que o push não faz; os dois convivem (o Telegram só age no modo ausente) |

---

## User Stories

### P1: Notificação quando uma sessão aguarda ⭐ MVP

**User Story**: Como dono da máquina, quero uma notificação no celular quando um agente parar pedindo permissão, mesmo com a tela bloqueada, para não deixar trabalho travado.

**Acceptance Criteria**:

1. WHEN uma sessão ou um subagente passa a **aguardando** THEN o servidor SHALL enviar um push a todos os aparelhos inscritos, detectado no próprio servidor (sem depender de página aberta).
2. A notificação SHALL dizer o nome da sessão (`Name`, ou o projeto) e "aguardando permissão" (ou "subagente aguardando"), e nada além disso.
3. WHILE a sessão segue aguardando THEN o servidor SHALL reenviar a cada 5 min.
4. Notificações da mesma sessão SHALL usar a mesma `tag`: a nova substitui a anterior, sem empilhar, e volta a vibrar (`renotify`).
5. WHEN toco na notificação THEN o app (ou a aba) do painel SHALL abrir ou ganhar foco.
6. WHEN o serviço de push responde 404 ou 410 THEN a inscrição SHALL ser apagada (aparelho desinscrito ou app desinstalado).

**Independent Test**: Inscrever o celular, bloquear a tela, mandar um `PermissionRequest` via `curl` e receber a notificação; esperar 5 min e receber de novo; tocar e cair no painel.

### P1: Ligar e desligar por aparelho ⭐ MVP

**Acceptance Criteria**:

1. WHEN o navegador suporta push e a página está em HTTPS THEN o cabeçalho SHALL mostrar um botão de sino ao lado do de som; senão, o botão não aparece.
2. WHEN toco no sino desligado THEN a página SHALL pedir permissão de notificação, inscrever o aparelho e mandar a inscrição ao servidor; o sino fica ligado.
3. WHEN toco no sino ligado THEN a página SHALL desinscrever o aparelho e apagá-lo do servidor.
4. WHEN a permissão de notificação está negada THEN o sino SHALL indicar o bloqueio no título, sem tentar de novo.
5. WHEN a página carrega com o aparelho inscrito THEN ela SHALL reenviar a inscrição ao servidor (idempotente), para recuperar o caso de o servidor ter perdido o arquivo.
6. As inscrições SHALL sobreviver a reiniciar o servidor (arquivo no disco).

**Independent Test**: Ligar o sino, ver a permissão pedida e o sino ligado; reiniciar o serviço e mandar um evento de teste: a notificação chega.

### ~~P2: Aposentar o modo `permission` do Telegram~~ (cancelada)

A decisão inicial (push substitui o Telegram) foi tomada achando que o modo `permission` só avisava. Ao conferir o script antes de removê-lo, ficou claro que ele permite aprovar e negar pelo Telegram, e só no modo ausente. Decisão do usuário em 2026-10-16: manter o Telegram; os dois convivem.

---

## Edge Cases

- WHEN o servidor sobe THEN ele SHALL NOT notificar o que já estava aguardando antes (as sessões redescobertas nascem em "desconhecido"; não há linha de base a perder).
- WHEN o diretório de estado não pode ser lido ou escrito THEN o push SHALL ficar desligado (API responde 503, sino some) e o painel segue funcionando.
- WHEN a inscrição traz um `endpoint` que não é HTTPS THEN o servidor SHALL recusá-la (400): ele só faz POST para serviços de push.
- WHEN o envio falha por rede ou 5xx THEN o servidor SHALL registrar no log e tentar de novo só no próximo lembrete, sem travar o Store.
- WHEN a sessão sai de aguardando THEN o lembrete SHALL parar; a notificação já mostrada fica até eu tocar ou descartar.

---

## Design

### Servidor

- **`internal/push`** (novo, só stdlib):
  - `vapid.go`: par P-256 em `<state>/vapid.pem` (gerado na primeira vez, 0600). JWT ES256 com `aud` = origem do endpoint, `exp` = agora + 12 h, `sub` = URL do repositório; cabeçalho `Authorization: vapid t=<jwt>, k=<chave pública>`.
  - `encrypt.go`: RFC 8291 (`aes128gcm`, registro único, `rs` 4096): ECDH efêmero com a `p256dh` do aparelho, HKDF com o `auth`, AES-128-GCM. Testado contra o exemplo do apêndice A da RFC.
  - `subscriptions.go`: inscrições em `<state>/push-subscriptions.json` (0600, escrita atômica), chave = `endpoint`.
  - `sender.go`: POST com `TTL`, `Urgency: high` e `Topic` por sessão; 201 = ok; 404/410 apaga a inscrição.
  - `notifier.go`: assina o Store (`Subscribe`), monta o conjunto de chaves aguardando (`<sessão>` e `<sessão>/<subagente>`), envia ao surgir uma chave nova e reenvia a cada 5 min (ticker de 30 s) enquanto ela seguir aguardando. Envio fora do lock do Store, numa goroutine.
- **API** (com token, como o resto): `GET /api/push/key`, `POST /api/push/subscription`, `DELETE /api/push/subscription` e `POST /api/push/test` (envia uma notificação de teste). Corpos JSON de até 4 KiB. Com o cookie `SameSite=Lax`, um POST vindo de outro site não leva o cookie.
- **`main`**: flag `-state-dir` (padrão `~/.config/claude-fleet`). Falha ao preparar o push só desliga o push.

### Página e service worker

- **Sino** no cabeçalho (área `push`), mesmo estilo do botão de som; some na horizontal junto com o cabeçalho.
- **`sw.js`** ganha `push` (mostra a notificação com `tag`, `renotify`, ícone) e `notificationclick` (foca uma janela do painel ou abre `/`). Continua sem `fetch` e sem cache.

## Tasks

1. `internal/push`: VAPID, criptografia (com o vetor da RFC), inscrições, envio — com testes.
2. Notifier: detecção de entrada em aguardando e lembrete de 5 min — com testes de relógio falso.
3. API de push no `internal/web` e flag `-state-dir` no `main` — com testes.
4. Sino na página e `push`/`notificationclick` no `sw.js`.
5. Deploy, teste de ponta a ponta no celular bloqueado, README.
6. ~~Desligar o modo `permission` do `telegram-notify`~~ — cancelada.

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| PUSH-01 | P1: Detecção no servidor e envio ao entrar em aguardando | Execute | Verified |
| PUSH-02 | P1: Conteúdo mínimo e `tag` por sessão | Execute | Verified |
| PUSH-03 | P1: Lembrete a cada 5 min | Execute | Verified |
| PUSH-04 | P1: Toque abre o painel | Execute | Verified |
| PUSH-05 | P1: 404/410 apaga a inscrição | Execute | Verified |
| PUSH-06 | P1: Sino liga/desliga por aparelho, com permissão negada tratada | Execute | Verified |
| PUSH-07 | P1: Inscrições persistidas e reenviadas ao carregar | Execute | Verified |
| PUSH-08 | P1: Web Push só com stdlib (VAPID + RFC 8291) | Execute | Verified |
| PUSH-09 | ~~P2: Desligar o modo `permission` do `telegram-notify`~~ | - | Cancelled |
