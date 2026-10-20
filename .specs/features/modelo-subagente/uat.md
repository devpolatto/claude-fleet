# modelo-subagente — UAT

**Data:** 2026-10-20 · **Base:** `main` em `f8e9b00` · **Navegador:** Chromium headless (Playwright)
**Ambiente:** servidor em `127.0.0.1:7799` com `-projects` apontando para transcripts sintéticos e um `SessionStart` enviado por `curl`. Nenhum dado real.

Legenda: ✅ verificado · ⚠️ desvio

## Cenário

| Subagente | Modelos no transcript |
| --- | --- |
| "mapear o código" | `claude-haiku-5-5` |
| "revisar a branch" | `claude-opus-5-5` (US$ 0,0424) e `claude-haiku-5-5` (US$ 0,0003) |
| "nome bem comprido…" | `claude-sonnet-4-6-20260101-experimental` (depois com um sufixo de 60+ caracteres) |
| "recém-lançado" | nenhuma resposta ainda |

## Resultado

| Critério | Resultado |
| --- | --- |
| MSA-01: "modelo haiku-5-5", antes de vida | ✅ |
| MSA-02: "modelo opus-5-5 +1" no agente com dois modelos | ✅ |
| MSA-03: `title` "claude-opus-5-5: US$ 0,0424 / claude-haiku-5-5: US$ 0,0003" | ✅ na primeira versão; o `title` saiu e o critério passou a valer pela lista (MSA-06) |
| MSA-04: "recém-lançado" sem o item | ✅ |
| MSA-05: desktop 1280 px, celular 360 px em pé, 812 × 375 deitado | ✅ `scrollWidth` igual à largura nos três; o nome de 60+ caracteres é cortado com reticências em 360 px |
| Erros no console | ✅ nenhum |

## Lista com um toque (MSA-06 a MSA-08)

**Data:** 2026-10-20 · **Base:** `main` em `d96b8fb` · mesmo cenário, Chromium headless com toque emulado (`hasTouch`, `isMobile`) em 360 × 760 e 812 × 375; mouse e teclado em 1280 × 900.

| Critério | Resultado |
| --- | --- |
| MSA-06: toque em "opus-5-5 +1" abre "claude-opus-5-5 US$ 0,0424 / claude-haiku-5-5 US$ 0,0003"; Enter no desktop fecha | ✅ |
| MSA-07: depois de um poll (6 s) a lista segue aberta e o foco segue no botão (`data-foco` = `modelo:b2`) | ✅ nas três larguras |
| MSA-08: `aria-expanded` alterna; alvo de 130 × 28 px; o nome de 60+ caracteres quebra em três linhas na lista em 360 px | ✅ `scrollWidth` igual à largura nas três |
| MSA-08, alvo com a `.meta` quebrada em 360 px: `elementFromPoint` em cada pixel da altura do botão | ✅ 29/29 px caem no botão. A primeira versão (`padding`/`margin` de 6 px fixos) dava 24/29: os 5 px de baixo caíam no `span` da linha seguinte; o alvo passou a sair de `--toque-min` com `1lh` e `z-index` (achado do `reviewer`) |
| Erros no console | ✅ nenhum |
