# estilo-hud Specification

> Status: **implementado e verificado** (2026-11-12), branch `feat/ui-hud`. UAT em [`uat.md`](uat.md).

Proposta visual no Figma: página "Página principal — estilo HUD" (antes "Proposta — estilo das referências") ([celular 56-466](https://www.figma.com/design/tVFsjlauIBbPrj2HSXNron/claude-fleet-%E2%80%94-painel-v2?node-id=56-466), [desktop 56-777](https://www.figma.com/design/tVFsjlauIBbPrj2HSXNron/claude-fleet-%E2%80%94-painel-v2?node-id=56-777)), derivada das imagens de referência do nó [49-466](https://www.figma.com/design/tVFsjlauIBbPrj2HSXNron/claude-fleet-%E2%80%94-painel-v2?node-id=49-466).

## Problem Statement

O painel funciona, mas a linguagem visual é genérica. O dono quer a página principal com cara de painel de comando ("HUD"): fundo quase preto com grade, tipografia mono técnica, números grandes, cantos retos e brilho só no acento, seguindo as referências reunidas no Figma.

## Goals

- [x] A página principal (cabeçalho, contadores, filtro e grade de tiles) segue a proposta do Figma no celular e no desktop.
- [x] Nenhuma função muda: mesmos dados, mesmos estados, mesmas interações.

## Out of Scope

| Feature | Reason |
| --- | --- |
| Redesenho do pop-up de detalhe, da tela de uso e do `login.html` | Pedido foi só a página principal. Herdam os tokens novos de `:root` (cores), sem retrabalho de layout |
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

---

## Success Criteria

- [x] Página principal conferida no navegador lado a lado com o Figma, no celular e no desktop.
- [x] Figma atualizado com o que mudou na implementação (contraste, tamanhos mínimos).
