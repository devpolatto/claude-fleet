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
| MSA-03: `title` "claude-opus-5-5: US$ 0,0424 / claude-haiku-5-5: US$ 0,0003" | ✅ |
| MSA-04: "recém-lançado" sem o item | ✅ |
| MSA-05: desktop 1280 px, celular 360 px em pé, 812 × 375 deitado | ✅ `scrollWidth` igual à largura nos três; o nome de 60+ caracteres é cortado com reticências em 360 px |
| Erros no console | ✅ nenhum |
