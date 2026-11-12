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

---

# Segunda etapa — pop-up, uso do plano e estados

**Data:** 2026-11-12 · **Base:** `feat/ui-hud` sobre `4c50722` · **Navegador:** Chromium headless shell (Playwright)
**Referência visual:** arquivo Penpot local "claude-fleet — painel v2" (páginas `HUD · Detalhes`, `HUD · Uso do plano`, `HUD · Estados` e `Biblioteca`), exportado em PNG e comparado lado a lado. O Figma **não** foi atualizado nesta etapa: o desenho dela vive no Penpot.
**Ambiente:** servidor em `127.0.0.1:7841` com `-state-dir` vazio e duas contas sintéticas (`-account pessoal=… -account trabalho=…`, diretórios vazios); um segundo servidor em `127.0.0.1:7842` com `-projects` vazio para o caso de uma conta. Sete sessões fictícias por `POST /event` (payloads de `internal/ingest/testdata/`), transcripts sintéticos só com `usage` e uso do plano por `POST /statusline` (ok 42 %/24 %; crítico 97 %, alerta 85 % e janelas reiniciadas). Nenhum dado real. Larguras: desktop 1280, celular 390 vertical e 844×390 horizontal.

| Caso | Esperado | Resultado |
| --- | --- | --- |
| Folha/painel (HUD-10) | painel de 520 px no desktop e folha no celular, borda `--border-forte`, halo leve, cantoneiras brancas de 14 px com brilho (`.cantos`); no celular as cantoneiras não saem da tela | ✅ |
| Cabeçalho do pop-up (HUD-11) | status e "▲ N subagente aguardando" empilhados à esquerda, ações à direita; título 20 px/600 em largura total; meta = chip de conta reto · projeto · ID curto (faint) · local do tmux | ✅ |
| Ações (HUD-11) | Terminal, Silenciar, Ocultar e Fechar retos de 32 px, ícone 14 px `--muted` e rótulo em maiúsculas; "Reativar som" em âmbar; terminal desabilitado a 60 %; abaixo de 600 px Silenciar e Terminal só com ícone | ✅ (terminal forçado no console: sem tmux no ambiente de teste, `det.terminal` preenchido à mão e `renderDetalhe()`) |
| Custo, dados, tabelas (HUD-12) | custo com borda forte e halo, número leve de 28 px; dados em 2×2 nas três larguras, sem cantoneiras, "tempo de vida" com quadrado `--ativo`; tabelas em caixa com cabeçalho em rótulo e linha do subagente em aguardando em âmbar | ✅ |
| Subagentes e avisos (HUD-13) | cartão em aguardando com fundo/borda/cantoneiras âmbar e botão de silenciar 28 px após o status; ativo e concluído só com a borda, concluído a 70 %; "EM FOCO" na linha do tipo; aviso de terminal em faixa âmbar com triângulo | ✅ |
| Uso do plano (HUD-14) | título em rótulo; cartão por conta com chip, "ATUALIZADO …", borda forte, halo e cantoneiras brancas; medidor com número leve na cor do estado, trilho de 4 px e barra com brilho; "reiniciada · aguardando dado novo" em `--faint`; tabela na largura do cartão; uma conta = 720 px com sessão e semana lado a lado; celular deitado = cartões lado a lado | ✅ |
| Estados (HUD-15) | botões do cabeçalho já seguiam os 6 estados; tile oculto 55 %, tracejado, olho cortado no canto inferior direito; chip de ocultas ligado com contorno `--acento`; alto-falante cortado em âmbar ao lado da etiqueta do tile e do mini-tile | ✅ |
| Texto e layout | menor fonte renderizada 10 px; `scrollWidth` igual à largura em 1280, 390 e 844×390; nenhum erro de JS | ✅ |
| Checks | `gofmt -l .` vazio, `go vet`, `go test -race` | ✅ |

## Desvios (segunda etapa)

- ⚠️ MODELOS segue como tabela (modelo, tokens, custo, acerto de cache), não como chips: os chips do Penpot perderiam custo e cache por modelo. A tabela ganhou o mesmo desenho da de tokens.
- ⚠️ No cartão de subagente, as linhas de meta seguem VIDA · TOKENS · ÚLTIMA e a lista de modelos com custo; o "MODELO haiku-5-5 +1" do Penpot não foi copiado para não duplicar a lista.
- ⚠️ Âmbar na tabela e no cartão marca o subagente **em aguardando**; o "em foco" (aberto pelo mini-tile) ganhou só o chip "EM FOCO", borda `--border-forte` e fundo `--surface-2` na linha da tabela, para o âmbar não aparecer num subagente ativo.
- ⚠️ Folha do celular com cantos retos (`--raio`), sem o raio de 20 px de antes.
- ⚠️ Tokens novos: `--canto-brilho` (`--text` a 55 %) e `--halo-leve` (`--text` a 6 %); `--uso-barra-h` passou de 10 px para 4 px.
