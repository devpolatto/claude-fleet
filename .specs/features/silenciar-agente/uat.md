# silenciar-agente — UAT

**Data:** 2026-10-20 · **Base:** `feat/silenciar-agente` em `1c720e8` · **Navegador:** Chromium headless (Playwright)
**Ambiente:** servidor em `127.0.0.1:7811` com `-projects` e `-state-dir` vazios; eventos de hook por `POST /event` para as sessões sintéticas `sess-alfa`, `sess-beta` (com o subagente `agent-1`) e `sess-gama`. Bipes contados interceptando `OscillatorNode.start` (880 Hz). Nenhum dado real.

Legenda: ✅ verificado · ⚠️ desvio

| Caso | Esperado | Resultado |
| --- | --- | --- |
| Entrada em aguardando | 1 bipe | 1 bipe ✅ |
| Silenciar alfa no pop-up | `aria-pressed=true`, pop-up aberto, tile segue aguardando com ", alerta silenciado" (SIL-01, SIL-07, SIL-10) | ✅ |
| Lembrete com beta e o subagente audíveis | toca (SIL-03) | 1 bipe em 36 s ✅ |
| Silenciar a sessão beta | botão do subagente segue sem silêncio (SIL-05) | ✅ |
| Silenciar o subagente | `aria-pressed=true` com `aria-label` fixo "Silenciar Explore", foco preservado no clique e no poll de 5 s, ícone no mini-tile (SIL-02, SIL-10) | ✅ |
| Os três silenciados | lembrete não toca (SIL-04) | 0 bipes em 36 s ✅ |
| `PostToolUse` em alfa | silêncio cai e a marca some (SIL-08) | ✅ |
| Novo `PermissionRequest` em alfa | toca de novo (SIL-08) | 1 bipe ✅ |
| Desfazer pelo mesmo botão | `aria-pressed` volta a `false` (SIL-06) | ✅ |
| Alfa fora de aguardando | botão some (SIL-01) | ✅ |
| Som global desligado, espera nova | nada toca (SIL-07) | 0 bipes ✅ |
| Recarregar | nada silenciado (SIL-09) | ✅ só em memória, sem `localStorage` |
| Checks | `gofmt -l .` vazio, `go vet`, `go test -race` | ✅ |

## Layout

Desktop 1280, celular vertical 390 e horizontal 844×390 com toque: sem rolagem horizontal; marcas de silenciada e oculta lado a lado, sem sobrepor. Sem animação nova (`prefers-reduced-motion` sem efeito a respeitar).

## Desvios

- ⚠️ Abaixo de 600 px o botão "Silenciar" do pop-up mostra só o ícone (`aria-label` e `title` completos): com três rótulos o aviso "1 subagente aguardando" ficava espremido.
- ⚠️ Os controles só aparecem com Web Audio disponível: sem áudio não há o que silenciar.
- A reconexão do SSE foi conferida só no código (o modo offline do Playwright não derruba o SSE no loopback); a poda num snapshot normal foi exercitada: alfa saiu de aguardando e perdeu o silêncio, beta o manteve.

## Figma

Componentes `silenciar (pop-up)`, `silenciar-sub` e `marca-silenciada`, e as telas de grade (desktop), detalhe (celular) e cabeçalho do pop-up nos quatro estados: [painel v2, nó 41-471](https://www.figma.com/design/tVFsjlauIBbPrj2HSXNron/claude-fleet-%E2%80%94-painel-v2?node-id=41-471).
