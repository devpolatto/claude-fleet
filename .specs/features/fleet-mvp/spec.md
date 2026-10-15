# fleet-mvp Specification

## Problem Statement

Rodo várias sessões do Claude Code em paralelo (terminais diferentes, subagentes em background) e não tenho uma visão única de quem está trabalhando, quem parou esperando minha permissão e quanto cada um está gastando. Quero acompanhar isso pelo celular, longe do computador, sem expor a máquina nem o conteúdo do trabalho.

## Goals

- [ ] Todo evento relevante de qualquer sessão/subagente se reflete no tile em ≤ 2 s.
- [ ] Tokens e custo estimado de uma sessão (principal + subagentes) visíveis em ≤ 2 toques, batendo com a soma do `usage` do transcript (diferença 0 em tokens).
- [ ] O hook nunca atrasa o Claude Code em mais de 1 s, nem com o servidor desligado.

## Out of Scope

| Feature | Reason |
| --- | --- |
| Ações sobre agentes (aprovar, interromper, enviar prompt) | Painel é somente leitura; ações exigem outro modelo de segurança |
| Mostrar prompts, comandos, saídas de ferramenta | Risco de vazamento de dados de trabalho; não é necessário para o objetivo |
| Histórico persistente e relatórios de custo | M1 é visão ao vivo; histórico fica para depois |
| Várias máquinas num painel | Uso pessoal, um host |
| Exposição na internet pública | Acesso remoto só por Tailscale |
| Faturamento real de assinatura | Custo é estimado pelo preço de lista da API |

---

## User Stories

### P1: Grade de agentes ao vivo ⭐ MVP

**User Story**: Como dono da máquina, quero ver todas as sessões do Claude Code como quadradinhos que mudam de estado em tempo real, para saber de relance quem está trabalhando.

**Why P1**: É o núcleo da ideia.

**Acceptance Criteria**:

1. WHEN uma sessão dispara `SessionStart` THEN o painel SHALL exibir um novo tile para ela, com o nome do projeto (basename do `cwd`) e o id curto (8 caracteres).
2. WHEN chega `UserPromptSubmit`, `PreToolUse` ou `PostToolUse` THEN o tile SHALL ficar **ativo** (animação de pulso) e mostrar o nome da última ferramenta.
3. WHEN chega `PermissionRequest` ou `Notification` THEN o tile SHALL ficar **aguardando** (amarelo, sem pulso).
4. WHEN chega `Stop` THEN o tile SHALL ficar **parado** (cinza).
5. WHEN chega `SessionEnd` THEN o tile SHALL ficar **encerrado** (esmaecido) e sumir 15 min depois.
6. WHEN um evento chega para uma sessão desconhecida THEN o sistema SHALL criar a sessão a partir desse evento.
7. WHEN a página está aberta THEN as mudanças SHALL aparecer sem recarregar (push), em ≤ 2 s.

**Independent Test**: Com o servidor rodando, simular eventos com `curl` em sequência (start → prompt → tool → permission → stop → end) e ver o tile passar pelos cinco estados.

---

### P1: Subagentes dentro da sessão-pai ⭐ MVP

**User Story**: Como dono da máquina, quero ver os subagentes como mini-tiles dentro da sessão que os lançou, para saber quando trabalho em background ainda está rodando.

**Why P1**: O usuário pediu explicitamente terminal **e** subagente.

**Acceptance Criteria**:

1. WHEN chega `SubagentStart` com `agent_id` THEN o tile da sessão SHALL exibir um mini-tile ativo para o subagente, com o `agent_type`.
2. WHEN um evento de ferramenta chega com `agent_id` THEN a atividade SHALL ser atribuída ao subagente (mini-tile pulsa) e não só à sessão.
3. WHEN chega `SubagentStop` THEN o mini-tile SHALL ficar concluído.
4. WHEN a sessão-pai está **parada** mas um subagente segue ativo THEN o tile-pai SHALL indicar "subagente em background" (mini-tile pulsando).

**Independent Test**: Simular `SubagentStart`/`PreToolUse` com `agent_id`/`SubagentStop` via `curl` e observar o mini-tile.

---

### P1: Pop-up de detalhes ⭐ MVP

**User Story**: Como dono da máquina, quero tocar num tile e ver, na mesma tela, o tempo de vida, os tokens e o custo daquele agente, para ter noção de custo e desempenho.

**Why P1**: Pedido explícito para a primeira rodada.

**Acceptance Criteria**:

1. WHEN toco num tile de sessão THEN o sistema SHALL abrir um modal sobre a grade, sem navegar para outra página; WHEN toco em X, fora do modal ou aperto Esc THEN ele SHALL fechar.
2. O modal SHALL mostrar: status, projeto e `cwd`, início, **tempo de vida** contando ao vivo, última atividade ("há 12 s"), última ferramenta e modelo(s) usados.
3. O modal SHALL mostrar tokens separados em input, cache write (5m e 1h), cache read e output, mais o total, para a sessão principal, para cada subagente e para a soma.
4. O modal SHALL mostrar o **custo estimado em USD** por linha e no total, com o rótulo "estimativa · preço de lista da API".
5. O modal SHALL listar os subagentes com tipo, descrição (do `.meta.json`), status, tempo de vida, tokens e custo.
6. WHEN toco num mini-tile de subagente THEN o modal SHALL abrir focado nesse subagente.
7. WHILE o modal está aberto THEN os números SHALL se atualizar sozinhos (≤ 5 s).

**Independent Test**: Apontar o servidor para uma sessão real já encerrada e conferir que os totais do modal batem com um script independente que soma o `usage` do transcript (deduplicado por `message.id`).

---

### P1: Acesso seguro pelo celular ⭐ MVP

**User Story**: Como dono da máquina, quero abrir o painel no celular sem abrir porta para a internet e sem vazar o conteúdo do meu trabalho.

**Why P1**: Sem isso o painel não sai do desktop; e sem segurança ele não pode existir.

**Acceptance Criteria**:

1. WHEN o servidor sobe sem flags THEN ele SHALL ouvir só em `127.0.0.1`.
2. WHEN o bind não é loopback e nenhum token foi configurado THEN o servidor SHALL recusar subir, com mensagem explicando.
3. WHEN há token configurado THEN toda rota de leitura (página, SSE, API) SHALL exigir o token (query `?t=` na primeira visita, depois cookie `HttpOnly`); sem token → 401.
4. WHEN uma requisição para `/event` não vem do loopback THEN o servidor SHALL responder 403.
5. WHEN chega um payload de hook THEN o servidor SHALL guardar só metadados (ids, evento, nome da ferramenta, `cwd`, caminho do transcript) e SHALL descartar `prompt`, `tool_input`, `tool_response`, `message` e afins; o corpo nunca é logado.
6. A página SHALL ser usável numa tela de 360 px de largura, sem scroll horizontal.

**Independent Test**: Subir com `-addr 0.0.0.0:7777` sem token (deve falhar); com token, acessar pelo celular via Tailscale; `curl` de outro host em `/event` → 403; `grep` no log e na memória exportada (`/api/sessions`) não encontra o texto de um prompt de teste.

---

### P2: Instalação dos hooks sem risco

**User Story**: Como dono da máquina, quero um comando que registre os hooks no `settings.json` sem apagar os que já existem e mostrando o que vai mudar.

**Why P2**: Dá para instalar à mão; o instalador só reduz o risco de quebrar o settings.

**Acceptance Criteria**:

1. WHEN rodo `claude-fleet install-hooks` THEN o sistema SHALL fazer backup datado do `settings.json`, mostrar o diff e só gravar após confirmação.
2. O merge SHALL preservar todos os hooks existentes (ex.: `telegram-notify`, `bws-guard`) e ser idempotente (rodar duas vezes não duplica entradas).
3. WHEN rodo `claude-fleet uninstall-hooks` THEN só as entradas do claude-fleet SHALL ser removidas.

**Independent Test**: Rodar contra uma cópia do `settings.json` atual, conferir o diff e rodar de novo (deve dizer "nada a fazer").

---

### P2: Descoberta de sessões já abertas

**User Story**: Como dono da máquina, quero que, ao (re)iniciar o servidor, as sessões que já estavam rodando apareçam, para não ficar com o painel vazio até o próximo evento.

**Why P2**: O MVP funciona sem isso; só melhora a primeira impressão e o restart.

**Acceptance Criteria**:

1. WHEN o servidor inicia THEN ele SHALL varrer `~/.claude/projects/*/*.jsonl` modificados nos últimos 30 min e criar tiles com status **desconhecido** até o próximo evento.
2. WHEN chega o primeiro evento de uma sessão descoberta THEN ela SHALL assumir o status normal.

**Independent Test**: Reiniciar o servidor com uma sessão aberta e ver o tile surgir em estado "desconhecido".

---

### P3: Atalhos de operação

**User Story**: Como dono da máquina, quero filtros simples (esconder encerradas, ordenar por custo) e um resumo no topo (n ativos, n aguardando, custo do dia).

**Why P3**: Conveniência.

**Acceptance Criteria**:

1. WHEN ativo o filtro "esconder encerradas" THEN o painel SHALL ocultá-las e lembrar a preferência no navegador.
2. O cabeçalho SHALL mostrar contagem por status e a soma do custo estimado das sessões visíveis.

---

## Edge Cases

- WHEN o servidor está fora do ar THEN o hook SHALL terminar em ≤ 1 s com exit 0, sem mensagem no terminal do Claude.
- WHEN a última linha do transcript está pela metade (escrita em andamento) THEN o leitor SHALL ignorá-la e não avançar o offset além da última linha completa.
- WHEN uma linha do transcript não é JSON válido THEN o leitor SHALL pulá-la e contar o erro, sem derrubar a leitura.
- WHEN o mesmo `message.id` aparece em várias linhas (uma por bloco de conteúdo — observado em 76 de 127 mensagens) THEN o uso SHALL contar uma única vez (último registro vence).
- WHEN o modelo não está na tabela de preços (ex.: `<synthetic>`, modelo novo) THEN os tokens SHALL contar e o custo SHALL aparecer como "n/d" para aquela parte.
- WHEN `usage.speed == "fast"` THEN o custo daquela chamada SHALL ser multiplicado por 2.
- WHEN `cache_creation` não traz a divisão 5m/1h THEN o total de `cache_creation_input_tokens` SHALL ser tratado como 5m.
- WHEN o transcript foi apagado ou não existe THEN o modal SHALL mostrar "transcript indisponível" e manter status e tempo de vida.
- WHEN o usuário faz `/clear` (nova `session_id` no mesmo terminal) THEN a sessão antiga SHALL ficar parada até a limpeza e a nova SHALL ganhar tile próprio.
- WHEN uma sessão fica sem eventos por 12 h e não está encerrada THEN ela SHALL ser removida do painel.
- WHEN a conexão SSE cai (celular bloqueado, troca de rede) THEN a página SHALL reconectar sozinha e receber o snapshot completo.
- WHEN dois eventos da mesma sessão chegam fora de ordem por milissegundos THEN o estado SHALL seguir o timestamp de chegada (sem reordenação no MVP).

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| HOOK-01 | P1 Grade — hook envia evento ao servidor (≤ 1 s, exit 0 sempre) | Design | Pending |
| HOOK-02 | P1 Grade — hooks registrados nos 10 eventos sem remover os existentes | Design | Pending |
| INGEST-01 | P1 Acesso — `/event` só aceita loopback | Design | Pending |
| INGEST-02 | P1 Acesso — sanitização: só metadados, corpo nunca logado | Design | Pending |
| STATE-01 | P1 Grade — máquina de estados evento → status | Design | Pending |
| STATE-02 | P1 Subagentes — rastrear subagentes por `agent_id` | Design | Pending |
| STATE-03 | P1 Grade — limpeza (encerradas 15 min, inativas 12 h) | Design | Pending |
| STATE-04 | P1 Grade — sessão criada no primeiro evento desconhecido | Design | Pending |
| UI-01 | P1 Grade — grade responsiva (360 px) | Design | Pending |
| UI-02 | P1 Grade — visual por status (pulso, amarelo, cinza, esmaecido) | Design | Pending |
| UI-03 | P1 Grade — atualização por SSE com reconexão | Design | Pending |
| UI-04 | P1 Grade/Subagentes — conteúdo do tile + mini-tiles | Design | Pending |
| DET-01 | P1 Pop-up — modal na mesma página, abre/fecha | Design | Pending |
| DET-02 | P1 Pop-up — status, tempo de vida ao vivo, última atividade, modelos | Design | Pending |
| DET-03 | P1 Pop-up — tokens por categoria, por agente e somados | Design | Pending |
| DET-04 | P1 Pop-up — custo estimado com rótulo | Design | Pending |
| DET-05 | P1 Pop-up — lista de subagentes com detalhes; foco no subagente tocado | Design | Pending |
| DET-06 | P1 Pop-up — atualização ao vivo enquanto aberto | Design | Pending |
| TOK-01 | P1 Pop-up — leitura incremental do transcript + dedup por `message.id` | Design | Pending |
| TOK-02 | P1 Pop-up — transcripts de subagentes + `.meta.json` | Design | Pending |
| TOK-03 | P1 Pop-up — custo por mensagem via tabela de preço (fast 2x, n/d) | Design | Pending |
| TOK-04 | P1 Pop-up — total da sessão = principal + subagentes | Design | Pending |
| SEC-01 | P1 Acesso — bind padrão 127.0.0.1 | Design | Pending |
| SEC-02 | P1 Acesso — token obrigatório fora do loopback (cookie HttpOnly) | Design | Pending |
| SEC-03 | P1 Acesso — nenhuma rota expõe conteúdo | Design | Pending |
| INST-01 | P2 Instalação — install/uninstall com backup, diff, idempotência | Design | Pending |
| BOOT-01 | P2 Descoberta — varredura de transcripts recentes no start | Design | Pending |
| OPS-01 | P3 Atalhos — filtros e resumo no topo | - | Pending |

**Coverage:** 28 total, 27 mapped to tasks, 1 unmapped (OPS-01, P3 — fora do M1).

---

## Success Criteria

- [ ] No UAT, 2 sessões de terminal + 1 subagente em background aparecem corretamente no celular via Tailscale, com cada transição visível em ≤ 2 s.
- [ ] Tokens do modal = soma independente do transcript (diferença 0); custo coerente com a tabela de preços.
- [ ] Com o servidor parado, um prompt no Claude Code não demora mais que 1 s a mais para começar a responder.
- [ ] Nenhum trecho de prompt de teste aparece no log, na API ou na página.
