# uso-plano Specification

> Status: **spec aprovada** em 2026-10-16 (fonte pelo status line, AD-007; troca de tela com um dedo). Próximo passo: design.

## Problem Statement

O painel mostra tokens e custo estimado por sessão, mas não o que mais importa numa assinatura: quanto da janela de 5 h e do limite semanal do plano já foi usado, e quando cada um reinicia. Hoje isso exige abrir um terminal e digitar `/usage`. Quero ver esse número no celular, numa segunda tela do Fleet, a um gesto de distância da grade de sessões.

## Goals

- [ ] Do painel aberto, chegar ao percentual usado da janela de 5 h e do semanal em 1 gesto, com os mesmos números do `/usage` (diferença ≤ 1 ponto percentual quando o dado é recente).
- [ ] Ver quando cada janela reinicia (hora local e contagem regressiva).
- [ ] Nenhuma credencial lida pelo servidor e nenhum endpoint não documentado (AD-007).
- [ ] O binário segue sem dependências externas e o hook segue dentro das regras da AD-006.

## Out of Scope

| Feature | Reason |
| --- | --- |
| Limites semanais por modelo (Opus, Sonnet) | O status line só entrega `five_hour` e `seven_day`; ficam para depois, se a fonte passar a trazê-los |
| Ler o token OAuth e consultar `/api/oauth/usage` | Endpoint não documentado e exige ler `~/.credentials.json`; fica como alternativa registrada na AD-007, não como P1 |
| Comprar uso extra ou mudar de plano | O painel é somente leitura (AD-003) |
| Histórico do uso ao longo do tempo (gráfico) | Ideia diferida "Histórico em SQLite"; aqui só o valor atual |
| Alerta (som/push) ao passar de X% | Pode virar P3 depois de usar a tela por um tempo |
| Mostrar o status line no terminal | O script do Fleet não imprime nada; um status line próprio do usuário fica fora (ver Edge Cases) |

---

## Fonte dos dados — AD-007 (aprovada em 2026-10-16)

O Claude Code entrega ao comando de **status line** um JSON que inclui `rate_limits`, com o mesmo dado do `/usage`:

```json
"rate_limits": {
  "five_hour": { "used_percentage": 37, "resets_at": 1792171200 },
  "seven_day": { "used_percentage": 12, "resets_at": 1792603200 }
}
```

(`resets_at` em segundos Unix; os campos somem quando o plano não tem limite, por exemplo com chave de API.)

**Decisão:** um script `fleet-statusline.sh`, irmão do `fleet-hook.sh` e embutido no binário do mesmo jeito, configurado como `statusLine` no `~/.claude/settings.json` pelo `install-hooks`. Ele repassa o JSON ao servidor (`POST /statusline`, só loopback) e não imprime nada. O servidor guarda **só** `rate_limits` (lista branca, como o `ingest`) e descarta o resto.
**Por quê:** usa um recurso documentado do Claude Code, não lê credencial e encaixa no caminho que já existe (script no loopback → lista branca → Store → SSE).
**Trade-off:** o dado só se atualiza enquanto alguma sessão interativa está aberta e respondendo; com tudo parado, a tela mostra o último valor e há quanto tempo ele chegou. Ocupa o `statusLine` (o usuário hoje não tem nenhum).
**Alternativa descartada por ora:** ler o token OAuth em `~/.claude/.credentials.json` e consultar `GET https://api.anthropic.com/api/oauth/usage` (o que o `/usage` faz). Dá dado mesmo sem sessão aberta, mas o endpoint não é documentado e o servidor passaria a manusear uma credencial.

---

## User Stories

### P1: Uso do plano chega ao servidor ⭐ MVP

**User Story**: Como dono da máquina, quero que o Fleet saiba o uso atual do meu plano sem eu digitar `/usage`, para poder ver isso de longe.

**Acceptance Criteria**:

1. WHEN o Claude Code roda o status line THEN `fleet-statusline.sh` SHALL repassar o JSON a `POST /statusline` no loopback, com as mesmas regras do hook: `curl -m 1`, sem stdout nem stderr, sempre `exit 0`.
2. WHEN o servidor recebe o JSON THEN ele SHALL guardar apenas `five_hour` e `seven_day` (`used_percentage`, `resets_at`) e a hora do recebimento; nenhum outro campo (`cwd`, `transcript_path`, `session_id`, modelo, custo) SHALL ser gravado nem logado.
3. WHEN `POST /statusline` chega de fora do loopback THEN o servidor SHALL recusar (403), como o `/event`.
4. WHEN o valor muda THEN o servidor SHALL avisar a página por SSE em ≤ 2 s.
5. O último valor SHALL ser gravado em `<state>/uso-plano.json` (0600, escrita atômica) e relido ao subir, para a tela não nascer vazia depois de reiniciar o serviço.
6. WHEN rodo `claude-fleet install-hooks` THEN ele SHALL configurar o `statusLine` apontando para o script; `uninstall-hooks` SHALL removê-lo.

**Independent Test**: Mandar um JSON de status line por `curl` ao `/statusline`, ver o valor em `/api/state` (ou no SSE) e conferir que só `rate_limits` foi guardado; reiniciar o serviço e ver o valor de novo.

### P1: Segunda tela com o uso do plano ⭐ MVP

**User Story**: Como dono da máquina, quero arrastar a tela para o lado e ver o uso da janela de 5 h e do semanal, para decidir se continuo ou espero o reset.

**Acceptance Criteria**:

1. WHEN arrasto um dedo para a esquerda na grade THEN a página SHALL ir para a tela de uso; arrastar para a direita SHALL voltar. Pontinhos embaixo SHALL indicar a tela atual e também servir de toque para trocar.
2. A troca SHALL funcionar com o celular na vertical e na horizontal, e no desktop pelas setas ← → e pelos pontinhos.
3. A tela SHALL mostrar dois medidores, **Sessão (5 h)** e **Semana**, cada um com o percentual usado, a hora local do reset e a contagem regressiva ("reinicia em 2 h 14 min").
4. A tela SHALL mostrar há quanto tempo o dado chegou ("atualizado há 3 min").
5. Cores dos medidores SHALL sair dos tokens em `:root`, com um tom de alerta a partir de 80% e outro a partir de 95%.
6. A troca de tela SHALL respeitar `prefers-reduced-motion` (sem animação de deslize, troca direta).
7. O arrasto vertical (rolar a grade) SHALL continuar funcionando; só um gesto claramente horizontal troca de tela.
8. A página SHALL lembrar a última tela aberta no próprio aparelho.

**Decisão (2026-10-16):** arrasto com **um dedo**, não dois: é o padrão do celular e não briga com o zoom do navegador.

**Independent Test**: Com um valor injetado por `curl`, abrir o painel no celular, arrastar para a esquerda, ver os dois medidores com percentual, reset e "atualizado há"; girar o celular e repetir.

### P2: Tokens e custo das janelas, pelos transcripts

**User Story**: Como dono da máquina, quero ver abaixo dos medidores quantos tokens e quanto custo estimado gastei nas últimas 5 h e nos últimos 7 dias, para ter um número mesmo quando o dado do plano está velho.

**Acceptance Criteria**:

1. A tela SHALL mostrar tokens (entrada, cache, saída) e custo estimado (AD-005) somados de todos os transcripts nas janelas das últimas 5 h e dos últimos 7 dias.
2. A janela de 5 h SHALL usar o mesmo início da janela do plano quando houver (`resets_at − 5 h`); senão, as últimas 5 h corridas.
3. A soma SHALL deduplicar por `message.id` (L-001) e reaproveitar a leitura incremental do `internal/transcript`.

**Independent Test**: Comparar a soma com uma contagem manual sobre um diretório de fixtures em `testdata/`.

---

## Edge Cases

- WHEN ainda não chegou nenhum dado THEN a tela SHALL dizer "sem dado do plano ainda — abra uma sessão do Claude Code" em vez de medidores zerados.
- WHEN `resets_at` já passou THEN o medidor daquela janela SHALL mostrar "reiniciada, aguardando dado novo" em vez do percentual antigo.
- WHEN o JSON do status line vem sem `rate_limits` (chave de API, Bedrock, plano sem limite) THEN o servidor SHALL manter o último valor e não apagar nada.
- WHEN já existe um `statusLine` no `settings.json` que não é o do Fleet THEN `install-hooks` SHALL não sobrescrevê-lo e SHALL avisar, deixando a feature sem fonte.
- WHEN várias sessões abertas mandam o status line ao mesmo tempo THEN vale o recebido por último (os valores são da conta, iguais entre sessões).
- WHEN o servidor está fora THEN o status line SHALL terminar em ≤ 1 s sem afetar o Claude Code (AD-006).
- WHEN o arquivo de estado não pode ser lido ou gravado THEN a feature SHALL funcionar só em memória e registrar no log.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| USO-01 | P1: Script de status line repassa ao loopback, regras do hook | Specify | Pending |
| USO-02 | P1: Lista branca só com `rate_limits` | Specify | Pending |
| USO-03 | P1: `/statusline` só do loopback | Specify | Pending |
| USO-04 | P1: Valor novo por SSE e persistido no disco | Specify | Pending |
| USO-05 | P1: `install-hooks`/`uninstall-hooks` cuidam do `statusLine` | Specify | Pending |
| USO-06 | P1: Segunda tela por arrasto horizontal, pontinhos e setas | Specify | Pending |
| USO-07 | P1: Medidores com percentual, reset, contagem e "atualizado há" | Specify | Pending |
| USO-08 | P1: Tokens de cor, `prefers-reduced-motion`, vertical e horizontal | Specify | Pending |
| USO-09 | P2: Tokens e custo das janelas pelos transcripts | Specify | Pending |
