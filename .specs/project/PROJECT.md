# claude-fleet

**Visão:** Painel web leve, acessível pelo celular, que mostra em tempo real todas as sessões do Claude Code e seus subagentes rodando na máquina — um quadradinho por agente, piscando quando está trabalhando — com um pop-up de detalhes (tempo de vida, tokens e custo estimado).
**Para:** O próprio dono da máquina (uso pessoal, um único host Linux).
**Resolve:** Com várias sessões e subagentes rodando em paralelo, não há uma visão única de quem está trabalhando, quem está esperando permissão e quanto cada um está consumindo — hoje isso exige alternar entre terminais.

## Goals

- Saber, olhando o celular, o estado de cada sessão/subagente com atraso ≤ 2 s entre o evento no Claude Code e a mudança no tile.
- Saber o consumo de tokens e o custo estimado de qualquer sessão (e de cada subagente) em ≤ 2 toques.
- Custo zero de operação: um binário, sem banco, sem dependência externa, sem impacto perceptível na latência do Claude Code (hook ≤ 1 s mesmo com o servidor fora do ar).

## Tech Stack (proposta — ver AD-001 no STATE.md)

**Core:**

- Linguagem: Go 1.27 (stdlib apenas; `net/http`, `embed`, `encoding/json`)
- Frontend: uma página HTML + CSS + JS puro, embutida no binário via `embed`
- Transporte em tempo real: Server-Sent Events (SSE)
- Persistência: nenhuma (estado em memória; tokens relidos dos transcripts em disco)

**Integrações:** hooks do Claude Code (`~/.claude/settings.json`), transcripts JSONL em `~/.claude/projects/`, Tailscale para acesso remoto.

## Scope

**v1 (MVP) inclui:**

- Captura de eventos de todas as sessões via hooks globais (terminal + subagentes)
- Grade de tiles com status ao vivo (ativo / aguardando / parado / encerrado), subagentes aninhados
- Pop-up de detalhes na mesma tela: tempo de vida, tokens (input, cache write, cache read, output), custo estimado, lista de subagentes
- Acesso pelo celular de forma segura (bind local + Tailscale, token obrigatório fora do loopback)

**Fora de escopo explicitamente:**

- Controlar agentes pelo painel (enviar prompt, aprovar permissão, matar sessão) — o painel é somente leitura
- Exibir conteúdo de prompts, comandos ou respostas
- Histórico persistente / relatórios de custo por período
- Múltiplas máquinas agregadas num painel
- Exposição direta na internet (port-forward, domínio público)

## Constraints

- Timeline: sem data; construção em outro momento.
- Técnico: não pode quebrar nem atrasar o Claude Code; os hooks atuais (`telegram-notify`, `bws-guard`) devem continuar intactos.
- Segurança: os payloads dos hooks carregam prompts e comandos (inclusive de trabalho) — nada disso pode sair da máquina ou ficar gravado.
- Custo exibido é **estimativa pelo preço de lista da API**; em plano de assinatura não corresponde à fatura.
