# estilo-hud — UAT

**Data:** 2026-11-12 · **Base:** `feat/ui-hud` sobre `df57690` · **Navegador:** Chromium headless shell (Playwright)
**Ambiente:** servidor em `127.0.0.1:7833` com `-state-dir` vazio, primeiro com `-projects` vazio e depois com duas contas sintéticas (`-account pessoal=… -account trabalho=…`, diretórios vazios). Sete sessões fictícias criadas por `POST /event` a partir dos payloads de `internal/ingest/testdata/`: aguardando, ativo com subagente aguardando, ativo com subagente ativo e concluído, parado com 3 concluídos, dois parados, encerrado. Nenhum dado real.

Legenda: ✅ verificado · ⚠️ desvio

| Caso | Esperado | Resultado |
| --- | --- | --- |
| Tokens (HUD-01) | `:root` com os tokens da proposta; nenhuma cor literal fora de `:root` (o `rgba(...)` do pop-up virou token) | ✅ |
| Fundo (HUD-02) | grade de 24 px em gradiente no `body`; miras de 12 px a cada 192 px no `body::before` com máscara; sem imagem | ✅ |
| Cabeçalho (HUD-03) | marca, push, som e "● AO VIVO"; leituras SESSÕES / AGUARDANDO / CONTA com número grande; régua da grade com fatias aguardando/ativo e o botão "N oculta(s)" | ✅ |
| Filtro de conta | com 2 contas aparece a leitura CONTA; escolher "trabalho" filtra a grade, o valor ganha a cor da conta, AGUARDANDO segue contando todas as contas | ✅ |
| Ocultas | ocultar pelo pop-up gera "1 OCULTA"; o clique alterna `aria-pressed` e mantém o foco no botão recriado | ✅ |
| Tiles (HUD-04) | cantos de 2 px, cantoneiras em L, índice `#NN` (âmbar quando há espera), rótulos em maiúsculas, aguardando como bloco âmbar com triângulo | ✅ |
| Abas (HUD-05) | SESSÕES / USO com sublinhado no `--acento`; `aria-current`, setas do teclado e deslize de toque (`pointerdown`/`pointerup` de 300 px) trocam a tela | ✅ |
| Semântica (HUD-06) | âmbar só em aguardando, verde-água em ativo, `--critico` nos medidores de uso, ciano só na interface | ✅ |
| Texto e contraste (HUD-07) | menor fonte renderizada 10 px nas três larguras; `--faint` #626a7c 3,7:1 sobre `--bg` (antes 3,3:1), `--muted` 5,4:1, `--text` 15:1 | ✅ |
| Movimento (HUD-08) | com `reducedMotion: reduce` o anel do ativo fica estático (`animation-name: none`) e a régua não anima | ✅ |
| Layout (HUD-09) | `scrollWidth` igual à largura em 1280, 390 e 844×390; no celular horizontal o cabeçalho some e só os tiles ficam | ✅ |
| Pop-up e uso | herdam os tokens, legíveis no desktop e no celular, sem quebra | ✅ |
| Checks | `gofmt -l .` vazio, `go vet`, `go test -race` | ✅ |

## Desvios

- ⚠️ `--faint` ficou em #626a7c (a proposta tinha #4A5262, 2,5:1). Tokens novos além da proposta: `--ativo-glow`, `--ativo-halo`, `--ativo-anel`, `--ativo-zero`, `--aguardando-glow`, `--aguardando-halo`, `--cabecalho-bg`, `--backdrop`, `--texto-min`; `--uso-critico` passou a apontar para `--critico`.
- ⚠️ Rótulos de 9 px da proposta subiram para 10 px; "subagente aguardando" usa tracking de 6% (não 12%) para caber no tile de 175 px.
- ⚠️ A aba inativa usa `--muted` (AA) em vez de `--faint`.
- ⚠️ A leitura AGUARDANDO aparece sempre ("00" esmaecido); antes o chip só aparecia com espera.
- ⚠️ Mini-tile de subagente segue com 24 px de altura (a proposta tinha 22 px) para não encolher o alvo de toque.
- ⚠️ Fonte: `--mono` do sistema (no Figma, Roboto Mono como substituta). O tile desconhecido foi conferido só no CSS: o estado sintético não produziu uma sessão desconhecida.

## Figma

Página "Página principal — estilo HUD": [celular 56-466](https://www.figma.com/design/tVFsjlauIBbPrj2HSXNron/claude-fleet-%E2%80%94-painel-v2?node-id=56-466) e [desktop 56-777](https://www.figma.com/design/tVFsjlauIBbPrj2HSXNron/claude-fleet-%E2%80%94-painel-v2?node-id=56-777). `--faint` corrigido nas coleções "proposta-hud · :root" e "claude-fleet · :root" (esta agora espelha o `:root` novo), estilos `hud/rotulo` e `hud/status` em 10 px, aba inativa em `--muted`.
