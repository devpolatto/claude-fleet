# custo-por-modelo — UAT

**Data:** 2026-10-17 · **Base:** `main` em `07e5883` · **Navegador:** Chromium headless (Playwright)
**Ambiente:** servidor em `127.0.0.1:7799` com `-projects` apontando para transcripts sintéticos e um `SessionStart` enviado por `curl`. Nenhum dado real.

Legenda: ✅ verificado · ⚠️ desvio

## Cenário

| Origem | Modelo | Input | Cache 5m | Cache read | Output | Custo (API) |
| --- | --- | --- | --- | --- | --- | --- |
| Principal | `claude-opus-5-5` | 1.500 | 42.000 | 380.000 | 7.200 | US$ 0,436 |
| Principal | `modelo-experimental` (sem preço) | 500 | 0 | 0 | 800 | n/d |
| Subagente "mapear o código" | `claude-haiku-5-5` | 900 | 15.000 | 120.000 | 2.500 | US$ 0,004415 |
| Subagente "revisar a branch" | `claude-opus-5-5` | 100 | 8.000 | 60.000 | 1.500 | US$ 0,0824 |
| Subagente "revisar a branch" | `claude-haiku-5-5` | 50 | 1.000 | 9.000 | 400 | US$ 0,00042 |

## Resultado

| Linha | Esperado (à mão) | Na tela | |
| --- | --- | --- | --- |
| `claude-opus-5-5` | 500.300 tokens · US$ 0,5184 · 440.000 ÷ 491.600 = 89,5% | 500 k · US$ 0,5184 · 89,5% | ✅ |
| `claude-haiku-5-5` | 148.850 tokens · US$ 0,004835 · 129.000 ÷ 145.950 = 88,4% | 149 k · US$ 0,0048 · 88,4% | ✅ |
| `modelo-experimental` | 1.300 tokens · sem preço · 0 ÷ 500 = 0% | 1,3 k · n/d · 0% | ✅ |
| Total | 650.450 tokens · custo parcial · 569.000 ÷ 638.050 = 89,2% | 650 k · ≥ US$ 0,5232 · 89,2% | ✅ |

| Critério | Resultado |
| --- | --- |
| CPM-01 a CPM-03: linhas por modelo, soma com subagentes, linha Total | ✅ tabela acima |
| CPM-04: modelo sem preço mostra `n/d`, e o Total `≥ US$` | ✅ |
| CPM-05: rótulo de estimativa na legenda e no `title` da coluna Custo | ✅ |
| CPM-06: fórmula do cache conferida nas quatro linhas | ✅ |
| Ordem do mais caro para o mais barato | ✅ opus, haiku, experimental |
| Sem rolagem horizontal: desktop 1280 px, celular 360 e 375 px em pé, 812 × 375 deitado | ✅ `scrollWidth` igual à largura da janela nos quatro; a tabela cabe sem rolar por dentro (326/326 px em 360, 341/341 em 375) |
| Pior caso em 360 px: nome longo (`claude-opus-4-6-20260101`) e custo ≥ US$ 10 | ✅ o nome é cortado com reticências (nome inteiro no `title`) e a tabela segue em 326/326 px. A primeira medição, com a coluna do nome em 150 px, dava 330 px num espaço de 326; a coluna passou para 110 px |
| Erros no console | ✅ nenhum |
