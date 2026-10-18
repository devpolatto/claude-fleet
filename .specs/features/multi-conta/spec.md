# multi-conta Specification

> Status: **spec aprovada** em 2026-10-18 (AD-009 registrada no `STATE.md`). Design e tasks aprovados em 2026-10-18; implementada e verificada no UAT (`uat.md`).

## Problem Statement

Dá para usar duas assinaturas do Claude ao mesmo tempo na mesma máquina, por exemplo uma pessoal e uma de trabalho, separando a configuração com `CLAUDE_CONFIG_DIR`: um alias abre o `claude` com `CLAUDE_CONFIG_DIR=~/.claude-pessoal`, outro com `~/.claude-trabalho`, e cada diretório tem o próprio login, `settings.json`, `projects/` e histórico. O Fleet foi feito para um único `~/.claude`. Por isso, as sessões das outras contas não aparecem, porque o hook só está no `~/.claude/settings.json`. As sessões já abertas quando o servidor sobe também não são achadas, porque o Bootstrap só varre `~/.claude/projects`. Além disso, o uso do plano (`uso-plano`, AD-007) supõe um só plano. Quero ver as sessões de todas as contas na mesma grade, saber de que conta é cada uma e ver o uso de cada plano separado.

## Goals

- [ ] Sessões e subagentes de todas as contas configuradas aparecem na mesma grade; o filtro do cabeçalho diz que conta está em vista e o pop-up diz a conta de cada sessão.
- [ ] Instalar o hook numa conta nova é um comando: `CLAUDE_CONFIG_DIR=<dir> claude-fleet install-hooks`.
- [ ] O uso do plano é guardado e mostrado por conta, sem misturar os números de duas assinaturas.
- [ ] Com uma conta só (o caso de hoje), nada muda: mesma configuração, mesma tela.
- [ ] Nenhum dado novo vindo do hook além do que o `ingest` já aceita (AD-003); só stdlib.

## Out of Scope

| Feature | Reason |
| --- | --- |
| Escolher em que conta uma sessão ou subagente roda | O painel é somente leitura (AD-003). A conta é escolhida por quem abre o `claude` (alias ou `CLAUDE_CONFIG_DIR`), e os subagentes herdam a da sessão-mãe |
| Abrir sessão nova pelo painel | Mesmo motivo |
| Login, logout ou troca de conta | Fica no Claude Code (`/login`, `/logout`); o Fleet não toca credenciais |
| Compartilhar skills, memória ou MCPs entre os diretórios | É configuração do Claude Code (links simbólicos), não do Fleet |
| Agregar contas de várias máquinas | Ideia futura "Agregar várias máquinas" do ROADMAP |
| Custo separado por preço de plano | O custo continua sendo o preço de lista da API (AD-005), igual para qualquer conta |

---

## Como identificar a conta: AD-009 (aprovada em 2026-10-18)

**Decision:** A conta de uma sessão é o **diretório de configuração** de onde vem o transcript. Todo evento do hook traz `transcript_path`, que já é aceito pelo `ingest` e tem a forma `<config>/projects/<projeto>/<session_id>.jsonl`. O servidor compara esse caminho com as contas configuradas (`-account nome=dir`, repetível) e grava na sessão só o **nome** da conta. Sem `-account`, nenhuma sessão tem conta e a interface não mostra conta nenhuma (a "conta implícita" de que as outras specs falam é esse caso).
**Reason:** Não muda o hook nem a lista branca do `ingest`, não lê o ambiente da sessão e funciona também para as sessões achadas pelo Bootstrap, que não têm evento.
**Trade-off:** A conta precisa ser declarada no servidor, que roda como serviço systemd e não herda o `CLAUDE_CONFIG_DIR` de ninguém. Um diretório não declarado aparece como "outra" (ver Edge Cases).
**Alternative:** (a) O hook mandar `$CLAUDE_CONFIG_DIR` num cabeçalho. Seria campo novo vindo do cliente e mudaria o hook (AD-006), sem cobrir o Bootstrap. (b) Descobrir sozinho todos os `~/.claude*`. Seria mágico demais: pegaria backups e diretórios abandonados.
**Impact:** Flag nova `-account`; `Session` ganha `Account`; Bootstrap varre o `projects/` de cada conta; `install-hooks` respeita `CLAUDE_CONFIG_DIR`; `uso-plano` passa a guardar um valor por conta; o pop-up mostra a conta e o cabeçalho ganha um filtro por conta quando há mais de uma.

---

## User Stories

### P1: Hook instalado em cada conta ⭐ MVP

**User Story**: Como dono da máquina, quero instalar o hook do Fleet em cada diretório de configuração com um comando, para que as sessões de todas as contas mandem eventos.

**Acceptance Criteria**:

1. WHEN rodo `claude-fleet install-hooks` com `CLAUDE_CONFIG_DIR` definido THEN os padrões de `-settings` e `-dir` SHALL ser `$CLAUDE_CONFIG_DIR/settings.json` e `$CLAUDE_CONFIG_DIR/claude-fleet`, em vez de `~/.claude/...`. O mesmo vale para `uninstall-hooks`.
2. WHEN `-settings` ou `-dir` são passados THEN eles SHALL prevalecer sobre `CLAUDE_CONFIG_DIR`, como hoje prevalecem sobre `HOME`.
3. WHEN `CLAUDE_CONFIG_DIR` não está definido THEN o comportamento SHALL ser o de hoje.
4. A saída do `install-hooks` SHALL dizer qual `settings.json` vai alterar, antes de pedir confirmação.

**Independent Test**: `CLAUDE_CONFIG_DIR=$(mktemp -d) claude-fleet install-hooks -yes` grava o hook no `settings.json` do diretório temporário e não toca o `~/.claude/settings.json`; `hook/test.sh` cobre uma sessão rodando com esse diretório.

### P1: Sessões de todas as contas na grade, com a conta identificada ⭐ MVP

**User Story**: Como dono da máquina, quero ver as sessões das duas assinaturas na mesma grade e saber de que conta é cada sessão, para acompanhar tudo de um lugar só.

**Acceptance Criteria**:

1. O servidor SHALL aceitar `-account nome=dir` (repetível; `dir` aceita `~`). Sem nenhum, o servidor SHALL funcionar como hoje (`-projects`, padrão `~/.claude/projects`), sem conta em nenhuma sessão.
2. WHEN chega um evento cujo `transcript_path` está dentro de `<dir>/projects/` de uma conta THEN a sessão SHALL receber o `nome` dessa conta. O prefixo mais longo vence quando um diretório contém outro.
3. Os subagentes SHALL herdar a conta da sessão-mãe.
4. WHEN o servidor sobe THEN o Bootstrap SHALL varrer o `projects/` de **cada** conta e marcar as sessões encontradas com a conta correspondente.
5. A API (`/api/state` e SSE) SHALL expor só o nome da conta, nunca o caminho do diretório.
6. `-projects` SHALL continuar funcionando quando não há `-account` e SHALL ser recusado junto com `-account`, com mensagem dizendo para usar só `-account`.
7. WHEN a sessão tem conta THEN o pop-up SHALL mostrar o nome da conta (chip com cor tirada de tokens em `:root`) e o rótulo acessível do tile SHALL citá-la; o tile **não** mostra a conta, porque o filtro do cabeçalho já diz que conta está em vista. WHEN não há conta THEN a interface SHALL ficar como hoje.
   **Ajuste (2026-10-18):** a primeira versão tinha um chip da conta em cada tile; o usuário o tirou por ser redundante com o filtro, que por isso subiu para P1.
8. A identificação SHALL funcionar no celular, na vertical e na horizontal, sem depender só de cor (acessibilidade).

**Independent Test**: Subir o servidor com `-account pessoal=<tmp1> -account trabalho=<tmp2>` e mandar dois eventos por `curl` com `transcript_path` em cada diretório. `/api/state` mostra as duas sessões com `account` certo, e o pop-up de cada sessão mostra a conta.

### P1: Uso do plano por conta ⭐ MVP

**User Story**: Como dono da máquina, quero ver o uso da janela de 5 h e do semanal de cada assinatura separado, para saber qual conta ainda tem folga.

**Depende de**: `features/uso-plano` (especificada, não implementada). Esta história **altera** aquela spec: lá, "os valores são da conta, iguais entre sessões" passa a valer **por conta**.

**Acceptance Criteria**:

1. WHEN chega um JSON em `POST /statusline` THEN o servidor SHALL descobrir a conta pelo `transcript_path` do próprio JSON (mesma regra da história anterior), usar o caminho só para isso e guardar apenas `rate_limits` + nome da conta. O caminho continua sem ser gravado nem logado (USO-02).
2. O servidor SHALL guardar o último valor **por conta**, e o arquivo `<state>/uso-plano.json` SHALL ser um mapa por nome de conta.
3. A tela de uso SHALL mostrar um par de medidores (5 h e semana) por conta, com o nome da conta, cada um com o seu "atualizado há". Com uma conta só, a tela SHALL ficar como na spec `uso-plano`.
4. `install-hooks` SHALL configurar o `statusLine` no `settings.json` da conta em questão (critério 1 da primeira história).

**Independent Test**: Mandar dois JSONs de status line por `curl`, um por conta e com percentuais diferentes, e ver os dois pares de medidores com os números certos; reiniciar o serviço e ver os dois de novo.

### P1: Filtrar a grade por conta ⭐ MVP (era P2; ver o ajuste no critério 7 acima)

**User Story**: Como dono da máquina, quero mostrar só as sessões de uma conta, para olhar o trabalho sem as sessões pessoais (e vice-versa).

**Acceptance Criteria**:

1. WHEN há mais de uma conta THEN o cabeçalho SHALL ter um seletor "todas / <conta> / …".
2. A escolha SHALL ficar salva no próprio aparelho, como a sessão oculta (`features/ocultar-sessao`).
3. Sessão aguardando permissão de uma conta filtrada SHALL continuar disparando som e push (o filtro é só visual).

**Independent Test**: Com sessões nas duas contas, escolher uma e ver só os tiles dela; recarregar a página e ver o filtro mantido.

---

## Edge Cases

- WHEN o `transcript_path` não está dentro de nenhuma conta configurada THEN a sessão SHALL aparecer mesmo assim, com a conta "outra", e o servidor SHALL registrar no log, uma vez por diretório, que há uma conta não declarada (só o diretório de configuração, nunca o caminho do transcript).
- WHEN um evento chega sem `transcript_path` THEN a sessão SHALL manter a conta que já tem; se ainda não tem nenhuma, SHALL ficar sem conta até o próximo evento que traga o caminho.
- WHEN dois `-account` usam o mesmo nome ou o mesmo diretório THEN o servidor SHALL recusar a configuração ao subir, com mensagem clara.
- WHEN o nome da conta tem caractere fora de `[a-z0-9-]` ou passa de 20 caracteres THEN o servidor SHALL recusar ao subir (o nome vai para a tela e para o arquivo de estado).
- WHEN o `projects/` de uma conta não existe (conta declarada, ainda sem login) THEN o Bootstrap SHALL pular essa conta sem erro.
- WHEN `CLAUDE_CONFIG_DIR` aponta para `~/.claude` THEN o `install-hooks` SHALL se comportar como sem a variável (mesmo arquivo, sem duplicar o hook).
- WHEN o `transcript_path` usa link simbólico ou `..` THEN a comparação SHALL ser feita sobre o caminho limpo (`filepath.Clean`), sem resolver links (não vale a pena um `stat` por evento).
- WHEN a conta "outra" manda status line THEN o uso do plano dela SHALL ser descartado. Não dá para separá-lo de outra conta não declarada.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| CONTA-01 | P1: `install-hooks`/`uninstall-hooks` respeitam `CLAUDE_CONFIG_DIR` | Verified | Verified |
| CONTA-02 | P1: Flag `-account nome=dir`, validação e convivência com `-projects` | Verified | Verified |
| CONTA-03 | P1: Conta derivada do `transcript_path`, herdada pelos subagentes | Verified | Verified |
| CONTA-04 | P1: Bootstrap em todas as contas | Verified | Verified |
| CONTA-05 | P1: API expõe só o nome da conta | Verified | Verified |
| CONTA-06 | P1: Conta no pop-up e no rótulo acessível do tile; tile sem chip (Figma junto) | Verified | Verified |
| CONTA-07 | P1: Uso do plano guardado, persistido e mostrado por conta | Verified | Verified (`uso-plano`) |
| CONTA-08 | P1: Filtro da grade por conta, salvo no aparelho (era P2) | Verified | Verified |

**Coverage:** 8 total, 8 mapped to tasks (T1–T11); 8 verified (CONTA-07 com o `uso-plano`, 2026-10-18).

---

## Success Criteria

- [ ] Com duas contas abertas ao mesmo tempo, todas as sessões aparecem na grade com a conta certa, inclusive as que já estavam abertas quando o serviço subiu.
- [ ] Os medidores de uso de cada conta batem com o `/usage` rodado naquela conta (diferença ≤ 1 ponto percentual com dado recente).
- [ ] Com uma conta só, `go test` e `hook/test.sh` passam sem mudar nada na configuração existente.
