# estilo-hud Specification

> Status: página principal **implementada e verificada** (2026-11-12); pop-up, uso do plano e estados **implementados e verificados** (2026-11-12, segunda etapa). Branch `feat/ui-hud`. UAT em [`uat.md`](uat.md).

Segunda etapa: o desenho de referência passou a ser o arquivo Penpot local "claude-fleet — painel v2" (importado deste Figma), páginas `HUD · Sessões`, `HUD · Detalhes`, `HUD · Uso do plano`, `HUD · Estados` e `Biblioteca` (componentes `hud/*` e paleta `claude-fleet`). A `Page 1` guarda o desenho original e é a fonte da verdade do comportamento.

Proposta visual no Figma: página "Página principal — estilo HUD" (antes "Proposta — estilo das referências") ([celular 56-466](https://www.figma.com/design/tVFsjlauIBbPrj2HSXNron/claude-fleet-%E2%80%94-painel-v2?node-id=56-466), [desktop 56-777](https://www.figma.com/design/tVFsjlauIBbPrj2HSXNron/claude-fleet-%E2%80%94-painel-v2?node-id=56-777)), derivada das imagens de referência do nó [49-466](https://www.figma.com/design/tVFsjlauIBbPrj2HSXNron/claude-fleet-%E2%80%94-painel-v2?node-id=49-466).

## Problem Statement

O painel funciona, mas a linguagem visual é genérica. O dono quer a página principal com cara de painel de comando ("HUD"): fundo quase preto com grade, tipografia mono técnica, números grandes, cantos retos e brilho só no acento, seguindo as referências reunidas no Figma.

## Goals

- [x] A página principal (cabeçalho, contadores, filtro e grade de tiles) segue a proposta do Figma no celular e no desktop.
- [x] Nenhuma função muda: mesmos dados, mesmos estados, mesmas interações.
- [x] Pop-up de detalhe, tela de uso do plano e estados (ocultar, silenciar, botões do cabeçalho, terminal) no mesmo estilo HUD.

## Out of Scope

| Feature | Reason |
| --- | --- |
| Redesenho do `login.html` | Só herda as cores; o layout fica |
| Fonte remota (DIN, Share Tech Mono) | Invariante: frontend sem rede externa. Usa o `--mono` do sistema |
| Backend, SSE, hook | Mudança só visual |

---

## User Stories

### P1: Página principal no estilo HUD ⭐ MVP

**User Story**: Como dono da máquina, quero a página principal no estilo das referências, para o painel ter identidade própria.

**Acceptance Criteria**:

1. Tokens de `:root` trocados pelos da proposta (`--bg`, `--grade`, `--surface*`, `--border*`, `--text`, `--muted`, `--faint`, `--acento`, `--ativo*`, `--aguardando*`, `--parado`, `--desconhecido*`, `--concluido`, `--critico`, `--raio`, `--gap`, `--pad`); nenhuma cor literal nova fora de `:root`.
2. Fundo com grade fina e marcas de mira, só em CSS (gradientes/pseudo-elementos), sem imagem.
3. Cabeçalho: marca, botões de push e som, indicador ao vivo; contadores como blocos de número grande com rótulo pequeno em maiúsculas (SESSÕES / AGUARDANDO / CONTA).
4. Tiles com cantos de `--raio`, índice `#NN`, rótulos em maiúsculas com tracking largo, status como "● NOME", e brilho apenas no acento do estado ativo/aguardando.
5. A troca entre telas vira abas SESSÕES / USO com sublinhado de acento, mantendo o gesto de deslizar e a acessibilidade atual (`role`, `aria-*`).
6. A semântica das cores de status se mantém: laranja só para aguardando, verde-água para ativo, vermelho para crítico; o acento de interface é ciano.

### P1: Legibilidade e acessibilidade preservadas ⭐ MVP

**Acceptance Criteria**:

1. Nenhum texto abaixo de 10 px (a proposta tinha rótulos de 9 px).
2. `--faint` não fica com contraste menor que o atual sobre `--bg` (a proposta tinha 2,6:1; o atual é ~3,4:1); `--text` e `--muted` em AA (≥ 4,5:1).
3. Pulso e brilho animados respeitam `prefers-reduced-motion` (sem animação, brilho estático).
4. Sem rolagem horizontal no celular vertical (390) e horizontal (844×390); desktop 1280 conferido.

### P1: Pop-up de detalhe no estilo HUD

**User Story**: Como dono da máquina, quero o pop-up de detalhe com a mesma linguagem da grade, para o painel não mudar de cara ao abrir uma sessão.

**Acceptance Criteria**:

1. Folha no celular (alça, borda `--border-forte`, cantoneiras brancas luminosas nas pontas) e painel central de 520 px no desktop, sobre a grade escurecida.
2. Cabeçalho em duas partes: linha com status, aviso de subagente e ações (Terminal, Silenciar, Ocultar, Fechar); abaixo, título e meta (conta · nome · ID · local do tmux) em largura total.
3. Botões de ação retos (32 px, `--raio`), rótulo em maiúsculas; abaixo de 600 px, Silenciar e Terminal só com o ícone (`aria-label` completo).
4. Custo em destaque com borda forte e halo leve; células de dado em 2×2 (celular e desktop), sem cantoneiras; chips de modelo retos; tabela de tokens com cabeçalho em rótulo e a linha do agente em aguardando em âmbar.
5. Cartões de subagente: só o que está em aguardando tem cantoneiras (âmbar) e fundo âmbar; ativo e concluído só com a borda.
6. Avisos do pop-up (terminal aberto, falha) numa faixa âmbar com triângulo.

### P1: Uso do plano no estilo HUD

**User Story**: Como dono da máquina, quero a tela de uso do plano com os mesmos cartões e medidores do HUD.

**Acceptance Criteria**:

1. Título "USO DO PLANO" em rótulo; um cartão por conta, com chip de conta reto, "ATUALIZADO …" em rótulo, borda forte, halo leve e cantoneiras brancas luminosas.
2. Medidores: rótulo em maiúsculas, percentual em número grande na cor do estado (ok, alerta ≥ 80 %, crítico ≥ 95 %), trilho fino de 4 px com brilho na barra; "reiniciada · aguardando dado novo" em `--faint`.
3. Tabela de tokens ocupa a largura do cartão; no desktop com uma conta, sessão e semana lado a lado.
4. Celular deitado: cartões lado a lado, sem cabeçalho (como hoje).

### P2: Estados no estilo HUD

**Acceptance Criteria**:

1. Botões do cabeçalho nos 6 estados (push desligado/ligado/bloqueado, som desligado/ligado/bloqueado): ligado em âmbar, bloqueado com borda tracejada e 70 %.
2. Tile oculto: 55 %, borda tracejada e olho cortado no canto inferior direito (o índice ocupa o de cima). Chip de ocultas ligado com contorno no acento.
3. Marca de silenciado (alto-falante cortado) em âmbar ao lado da etiqueta do tile e do mini-tile.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| HUD-01 | Tokens novos em `:root` | Execute | Verified |
| HUD-02 | Fundo com grade e miras em CSS | Execute | Verified |
| HUD-03 | Cabeçalho e contadores em blocos | Execute | Verified |
| HUD-04 | Tiles no estilo HUD | Execute | Verified |
| HUD-05 | Abas SESSÕES / USO | Execute | Verified |
| HUD-06 | Semântica de status mantida | Execute | Verified |
| HUD-07 | Texto ≥ 10 px e contraste | Execute | Verified |
| HUD-08 | `prefers-reduced-motion` | Execute | Verified |
| HUD-09 | Layout celular (vertical/horizontal) e desktop | Execute | Verified |
| HUD-10 | Pop-up: folha/painel com cantoneiras | Execute | Verified |
| HUD-11 | Pop-up: cabeçalho em duas partes e ações retas | Execute | Verified |
| HUD-12 | Pop-up: custo, dados 2×2, modelos, tabela | Execute | Verified |
| HUD-13 | Pop-up: subagentes (cantoneiras só em aguardando) e avisos | Execute | Verified |
| HUD-14 | Uso do plano: cartões, medidores e tabela | Execute | Verified |
| HUD-15 | Estados: botões do cabeçalho, oculto, silenciado | Execute | Verified |

---

## Success Criteria

- [x] Página principal conferida no navegador lado a lado com o Figma, no celular e no desktop.
- [x] Figma atualizado com o que mudou na implementação (contraste, tamanhos mínimos).
- [x] Pop-up, uso do plano e estados conferidos no navegador lado a lado com o Penpot, no celular e no desktop.
