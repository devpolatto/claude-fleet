# pwa Specification

> Status: **P1 implementada e verificada** em 2026-10-16: instalada no GNOME pelo Chrome/Brave e acessada por HTTPS no Chrome do Android. P2 (service worker) não implementada.

## Problem Statement

Abro o painel no navegador do celular e do desktop digitando o endereço ou procurando a aba. Quero instalá-lo como aplicativo, do jeito que o Teams se instala pelo Brave: ícone na tela inicial do celular e no dock do GNOME, abrindo em janela própria, sem a barra do navegador. Hoje o Chrome só oferece um atalho que abre numa aba comum, porque a página não tem manifesto e é servida por HTTP.

## Goals

- [ ] O Chrome (Android) e o Chrome/Brave (desktop) oferecem "Instalar app" para o painel, e o app instalado abre em janela própria (`display: standalone`).
- [ ] O painel instalado continua só na tailnet, com o mesmo token, sem exposição na internet.
- [ ] O binário segue único e sem etapa de build: nada de framework nem `npm`.

## Out of Scope

| Feature | Reason |
| --- | --- |
| Reescrever a página em React (ou outro framework) | A instalação vem do manifesto, não do framework; React traria build e dependências sem capacidade nova (decisão de 2026-10-16) |
| Notificação push / alerta com a tela bloqueada | Próximo passo, que depende desta feature (HTTPS + service worker); fica no item do alerta do M2 |
| Funcionar offline | O painel só faz sentido conectado ao servidor; sem rede, mostra o "reconectando…" que já existe |
| Publicar em loja de apps (Play Store, TWA) | Uso pessoal |
| `tailscale serve` / `tailscale funnel` na frente do painel | O proxy faz toda requisição chegar como loopback e abriria o `/event` para a tailnet (ver README, seção 4) |

---

## User Stories

### P1: Painel instalável (manifesto e ícones) ⭐ MVP

**User Story**: Como dono da máquina, quero instalar o painel como app no celular e no desktop, para abri-lo por um ícone, em janela própria.

**Why P1**: É o pedido.

**Acceptance Criteria**:

1. WHEN a página carrega THEN ela SHALL declarar um manifesto (`<link rel="manifest">`) com `name` "Claude Fleet", `short_name`, `start_url` `/`, `scope` `/`, `display` `standalone`, `background_color` e `theme_color` iguais ao tema escuro (`--bg`), e ícones PNG de 192 e 512 px (o de 512 também `maskable`).
2. WHEN o navegador pede o manifesto ou os ícones THEN o servidor SHALL entregá-los **sem exigir token**: o navegador busca o manifesto sem cookies, e esses arquivos não contêm nada da máquina.
3. WHEN abro o app instalado THEN ele SHALL abrir em janela própria, com o ícone no lançador do sistema (tela inicial do Android, dock/atividades do GNOME).
4. A página SHALL ter `<meta name="theme-color">` e um `apple-touch-icon`, para a barra do sistema e para o caso de um iPhone.
5. O ícone SHALL reaproveitar a marca do cabeçalho (os quatro quadradinhos), sem marca de terceiros.

**Independent Test**: No desktop, abrir o painel por HTTPS no Chrome, ver o botão de instalar na barra de endereço, instalar e encontrar "Claude Fleet" nas atividades do GNOME; no Chrome do Android, "Instalar app" no menu e ícone na tela inicial.

---

### P1: HTTPS na tailnet ⭐ MVP

**User Story**: Como dono da máquina, quero o painel servido por HTTPS no nome da máquina na tailnet, porque o navegador só instala de verdade um app em contexto seguro.

**Why P1**: Sem HTTPS (ou `localhost`), o Chrome só cria um atalho que abre numa aba; não há instalação de verdade.

**Acceptance Criteria**:

1. WHEN o servidor recebe `-tls-cert` e `-tls-key` THEN ele SHALL servir HTTPS no endereço da tailnet (ex.: `https://meu-pc.tail1234.ts.net:7777`), usando um certificado emitido pelo Tailscale (`tailscale cert`).
2. O listener de loopback (`127.0.0.1:7777`) SHALL continuar em HTTP, para o hook não precisar de TLS; o `/event` continua aceitando só loopback.
3. WHEN o arquivo do certificado é renovado no disco THEN o servidor SHALL passar a usar o novo sem reiniciar (relido sob demanda, ex.: no `GetCertificate` quando o arquivo mudou).
4. WHEN o painel é servido por HTTPS THEN o cookie do token SHALL ganhar `Secure`.
5. WHEN só um dos dois flags é passado, ou os arquivos não podem ser lidos THEN o processo SHALL sair com erro claro, sem cair para HTTP em silêncio.
6. O `deploy/claude-fleet.service` e o README SHALL documentar: habilitar HTTPS no painel do Tailscale (MagicDNS + HTTPS Certificates), gerar o certificado e renová-lo (timer do systemd rodando `tailscale cert`, pois ele vale 90 dias).

**Independent Test**: Gerar o certificado com `tailscale cert`, subir com os dois flags, abrir `https://<nome>.ts.net:7777/?t=<token>` no celular, sem aviso de certificado; `curl` no `127.0.0.1:7777/event` continua funcionando.

---

### P2: Service worker mínimo

**User Story**: Como dono da máquina, quero um service worker registrado, para a instalação funcionar nos navegadores que ainda o exigem e para preparar as notificações push.

**Why P2**: O Chrome atual instala sem service worker (verificar no Execute); ele passa a ser obrigatório só com push.

**Acceptance Criteria**:

1. WHEN a página carrega por HTTPS THEN ela SHALL registrar `/sw.js`, servido sem token.
2. O service worker SHALL NOT guardar em cache a página, a API nem o `/stream`: ele não serve dados velhos e não guarda nada do trabalho no aparelho.
3. WHEN a página é servida por HTTP THEN ela SHALL NOT tentar registrar o service worker (o navegador recusaria).

**Independent Test**: No DevTools → Application, ver o service worker ativo e o Cache Storage vazio.

---

## Edge Cases

- WHEN abro o app instalado pelo ícone THEN o cookie do token SHALL ser enviado: ele usa `SameSite=Lax`, porque a abertura pelo ícone chega como navegação vinda de fora e um cookie `Strict` não ia junto (achado no teste de 2026-10-16: o app abria em "não autorizado").
- WHEN o app abre sem cookie válido (nunca gravado ou vencido) THEN a página SHALL mostrar a tela de login, onde colo o token; no app instalado não há barra de endereço para o `?t=`.
- WHEN abro `/?t=<token>` THEN o servidor SHALL gravar o cookie e redirecionar para `/`, para o token não ficar na barra nem no histórico.
- WHEN uso a página com cookie válido THEN a validade SHALL ser renovada: só vence depois de 30 dias sem uso.
- WHEN o app está aberto e o servidor reinicia THEN o comportamento SHALL ser o mesmo da aba: "reconectando…" e volta sozinho.
- WHEN o Chrome do celular usa **DNS seguro** com um provedor escolhido THEN o nome `*.ts.net` não resolve ("não foi possível encontrar o endereço"); a solução é "Usar o provedor atual" (achado no teste de 2026-10-16, documentado no README). O DNS privado do Android com servidor fixo tem o mesmo efeito.
- WHEN acesso pelo IP (`https://100.x.x.x`) em vez do nome THEN o certificado não vale; o README SHALL indicar sempre o nome `*.ts.net`.
- WHEN a tailnet ainda não tem HTTPS habilitado THEN `tailscale cert` falha; hoje `CertDomains` está vazio nesta máquina, então habilitar HTTPS Certificates no painel do Tailscale é pré-requisito.

---

## Design (como foi implementado)

- **Arquivos embutidos no binário** (`internal/web/static/`): `manifest.webmanifest`, `icon-192.png`, `icon-512.png` (também `maskable`) e `apple-touch-icon.png`. Rotas `GET` fora do `s.auth`, listadas em `publicAssets` (`internal/web/server.go`). O `icon-192.png` serve também de favicon.
- **TLS**: flags `-tls-cert`/`-tls-key` no `cmd/claude-fleet`. O `-addr` continua sendo o **IP** da tailnet (o navegador usa o nome, que o MagicDNS resolve para esse IP). `validateTLS` recusa TLS em loopback, `0.0.0.0`, porta sem host e nome, porque só com um IP específico o `listen` abre o `127.0.0.1` em HTTP para o hook.
- **Um `http.Server` por listener** (`serve` em `main.go`): um servidor compartilhado entre `Serve` e `ServeTLS` fecha a conexão HTTP/2 logo após o pedido quando o `Serve` sobe primeiro (achado no teste com `curl`; coberto por `TestServeTLSComHTTP2ELoopbackHTTP`).
- **Recarga do certificado**: `web.CertReloader` compara o `mtime` dos dois arquivos a cada handshake; se a releitura falhar, segue com o par anterior.
- **Certificado**: `tailscale cert` grava `~/.config/claude-fleet/tls.crt` e `tls.key` (600). O domínio fica em `CLAUDE_FLEET_TLS_DOMAIN`, no mesmo `env` do token. `deploy/claude-fleet-cert.timer` roda `claude-fleet-cert.service` toda semana.
- **Pré-requisito confirmado**: `sudo tailscale set --operator=$USER`, sem o qual o `tailscale cert` responde "Access denied".

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| PWA-01 | P1: Instalável — manifesto completo e `<link rel="manifest">` | Execute | Verified |
| PWA-02 | P1: Instalável — ícones 192/512 (maskable) e apple-touch-icon com a marca | Execute | Verified |
| PWA-03 | P1: Instalável — manifesto, ícones e sw servidos sem token | Execute | Verified |
| PWA-04 | P1: HTTPS — flags `-tls-cert`/`-tls-key` no listener da tailnet; loopback segue HTTP | Execute | Verified |
| PWA-05 | P1: HTTPS — recarrega certificado renovado sem reiniciar | Execute | Verified |
| PWA-06 | P1: HTTPS — cookie `Secure` e erro claro com configuração incompleta | Execute | Verified |
| PWA-07 | P1: HTTPS — serviço, timer de renovação e README | Execute | Verified |
| PWA-08 | P2: Service worker sem cache, só por HTTPS | - | Pending |

**Coverage:** 8 total, 7 implementados (P1), 1 pendente (P2).

---

## Success Criteria

- [ ] "Claude Fleet" instalado aparece nas atividades do GNOME e na tela inicial do Android, abrindo em janela própria.
- [ ] Zero avisos de certificado acessando pelo nome `*.ts.net`.
- [ ] O hook continua entregando eventos pelo loopback, sem mudança no `fleet-hook.sh`.
