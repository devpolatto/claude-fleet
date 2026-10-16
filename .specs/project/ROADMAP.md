# Roadmap

**Current Milestone:** M1 — MVP do painel
**Status:** M1 concluído (2026-10-15)

---

## M1 — MVP do painel

**Goal:** Abrir o painel no celular (via Tailscale), ver todas as sessões e subagentes piscando conforme trabalham e tocar num tile para ver tempo de vida, tokens e custo.
**Target:** Todos os critérios P1 de `.specs/features/fleet-mvp/spec.md` verificados no UAT (T18).

### Features

**fleet-mvp** - DONE (2026-10-15; UAT em `.specs/features/fleet-mvp/uat.md`)

- Coleta de eventos via hooks globais do Claude Code
- Máquina de estados de sessões e subagentes em memória
- Leitura incremental dos transcripts → tokens e custo estimado
- Grade de tiles ao vivo (SSE) + pop-up de detalhes
- Acesso remoto seguro (loopback por padrão, token + Tailscale)
- (P2) Instalador de hooks com backup e diff; descoberta de sessões já abertas

---

## M2 — Operação contínua

**Goal:** O painel fica sempre de pé e avisa quando precisa de você.

### Features

**Serviço systemd de usuário** - DONE (antecipado no M1: `deploy/claude-fleet.service`)
**Alerta "aguardando permissão"** (vibração/notificação no navegador; avaliar se substitui ou complementa o `telegram-notify`) - IN PROGRESS: som no navegador com toggle (`features/alerta-sonoro`); vibração e notificação do sistema seguem PLANNED
**Instalação como PWA** (ícone na tela inicial do celular e no GNOME; exige HTTPS via certificado do Tailscale) - IN PROGRESS: P1 (manifesto, ícones, HTTPS) DONE e verificada; falta a P2 (service worker). Spec em `features/pwa`
**Ocultar sessão da grade** (botão no pop-up; por nome, salvo no navegador; volta a aparecer quando aguarda permissão) - PLANNED: spec em `features/ocultar-sessao`

---

## Future Considerations

- Histórico em SQLite e gráfico de custo por dia/projeto
- Agregar várias máquinas (servidor central + agentes reportando)
- Ações remotas (aprovar permissão, interromper) — exige modelo de segurança próprio
- Detecção de sessão morta (processo encerrado sem `SessionEnd`) via PID ou inatividade do transcript
