# multi-conta Tasks

**Design**: `.specs/features/multi-conta/design.md`
**Status**: In Progress (aprovadas em 2026-10-18; fases 1 a 3 concluídas)

**Gates** (comandos do `CLAUDE.md`; não há `.specs/codebase/TESTING.md`):

| Gate | Comando |
| --- | --- |
| quick | `go test -race -count=1 ./...` |
| full | `gofmt -l .` vazio, `go vet ./...`, `go test -race -count=1 ./...`, `bash hook/test.sh` |
| browser | Painel no desktop e em largura de celular (vertical e horizontal), com uma e com duas contas |

Os testes de Go rodam em paralelo com segurança (cada teste usa `t.TempDir()`). O `hook/test.sh` sobe um servidor real numa porta, então roda sozinho.

---

## Execution Plan

### Phase 1: Fundação (Parallel OK)

```
T1 [P]    (internal/account)
T3 [P]    (install-hooks + CLAUDE_CONFIG_DIR)
T10 [P]   (spec do uso-plano por conta)
```

### Phase 2: Servidor (Sequential)

```
T1 ──→ T2 ──→ T4
```

### Phase 3: Integração e entrega (Parallel OK)

```
T3, T4 ──→ T5 [P]   (hook/test.sh)
T4 ──────→ T9 [P]   (deploy + README)
T2 ──────→ T6       (chip e pop-up)
```

### Phase 4: Interface (Sequential)

```
T6 ──→ T7 ──→ T8
```

### Phase 5: Fechamento

```
T5, T7, T8, T9, T10 ──→ T11
```

---

## Task Breakdown

### T1: Pacote `internal/account` [P]

**What**: `Account`, `List`, `Parse`, `Implicit`, `Resolve`, `ProjectsDirs` e `OnUnknown`, como no design.
**Where**: `internal/account/account.go`, `internal/account/account_test.go`
**Depends on**: None
**Reuses**: estilo de pacote-folha de `internal/pricing` (sem dependência interna)
**Requirement**: CONTA-02, CONTA-03

**Done when**:

- [x] `Parse` recusa: sem `=`, nome vazio, nome fora de `^[a-z0-9-]{1,20}$`, `outra`, nome repetido, diretório repetido (depois de `Clean`) e caminho relativo; expande `~/` com `home`
- [x] `Resolve` devolve a conta de prefixo mais longo, `""` na implícita e `"outra"` fora de todas; aplica `filepath.Clean` (um caminho com `..` que sai da conta não casa)
- [x] `<dir>/projectsX/…` não casa com `<dir>/projects` (prefixo por segmento, não por string)
- [x] `OnUnknown` é chamado uma vez por diretório de configuração, com o pai do segmento `projects` (nunca o caminho do transcript); seguro sob `-race` com chamadas concorrentes
- [x] Gate passa: `go test -race -count=1 ./internal/account/` com ≥ 6 funções de teste (`TestParseRecusaNomeInvalido`, `TestParseExpandeTil`, `TestParseRecusaRepetido`, `TestResolvePrefixoMaisLongoVence`, `TestResolveForaDasContasViraOutraELogaUmaVez`, `TestResolveLimpaPontoPonto`)

**Feito** (47fec57). Desvio do design: `Parse`/`Implicit` devolvem `*List` (há mutex dentro) e, no lugar de `ProjectsDirs()`, há `All()` + `Account.ProjectsDir()`, para o `main` logar o nome junto com o diretório. `List` nil resolve tudo para `""`.

**Tests**: unit · **Gate**: quick
**Commit**: `feat(account): contas do Claude Code por diretório de configuração`

---

### T2: Conta na sessão do Store

**What**: Opção `WithAccounts`; `Session.Account` e `SessionView.Account`; resolução em `Apply` (quando chega `TranscriptPath`) e em `Bootstrap`.
**Where**: `internal/store/store.go`, `types.go`, `bootstrap.go` e testes
**Depends on**: T1
**Reuses**: `mutation.setString`, bloco `if ev.TranscriptPath != ""` (`store.go:155`), criação de sessão em `bootstrap.go:57`
**Requirement**: CONTA-03, CONTA-04, CONTA-05

**Done when**:

- [x] `New()` sem opção segue igual: todos os testes atuais do `store` passam sem alteração
- [x] Evento com `transcript_path` dentro de uma conta grava `Account`; evento sem caminho mantém a conta; mudar de conta (resume em outro diretório) atualiza e notifica
- [x] `Bootstrap` marca a conta pelo diretório varrido
- [x] `Snapshot()` e `Get()` trazem `Account`; nenhum campo com o diretório
- [x] Gate passa: `go test -race -count=1 ./internal/store/` com os 4 testes novos (`TestApplyGravaContaPeloTranscript`, `TestApplySemTranscriptMantemConta`, `TestBootstrapMarcaContaPorDiretorio`, `TestSemContasNaoMudaNada`)

**Feito** (43da8ba). Resolver a conta acontece sob o lock do Store; o aviso de conta desconhecida (um `log.Printf`) roda ali dentro, uma vez por diretório.

**Tests**: unit · **Gate**: quick
**Commit**: `feat(store): conta da sessão pelo transcript_path`

---

### T3: `install-hooks` respeita `CLAUDE_CONFIG_DIR` [P]

**What**: Padrões de `-settings` e `-dir` saem de `$CLAUDE_CONFIG_DIR` quando definido; recusa caminho relativo ou com `~` literal; mostra `Conta: <dir>` antes do diff.
**Where**: `cmd/claude-fleet/install.go`, `install_test.go`
**Depends on**: None
**Reuses**: `parseHooksFlags` e o `getenv` injetável
**Requirement**: CONTA-01

**Done when**:

- [x] Com `CLAUDE_CONFIG_DIR=<tmp>`: grava em `<tmp>/settings.json` e `<tmp>/claude-fleet/`; o `HOME` fake fica intocado
- [x] `-settings`/`-dir` explícitos vencem a variável
- [x] `CLAUDE_CONFIG_DIR=~/x` e `CLAUDE_CONFIG_DIR=x` saem com 2 e mensagem que cita o `~`
- [x] `CLAUDE_CONFIG_DIR=$HOME/.claude` dá o mesmo resultado de sem a variável (sem hook duplicado)
- [x] `uninstall-hooks` usa a mesma regra
- [x] Gate passa: `go test -race -count=1 ./cmd/claude-fleet/` com os testes novos (`TestInstallHooksUsaClaudeConfigDir`, `TestInstallHooksRecusaConfigDirRelativo`)

**Feito** (56749a2). Desvio: a linha `Conta: <dir>` não foi acrescentada; o `Mudanças em <settings>` que já sai antes do diff diz qual arquivo muda (critério 4 da spec), e o teste confere isso.

**Tests**: unit · **Gate**: quick
**Commit**: `feat(cli): install-hooks na conta do CLAUDE_CONFIG_DIR`

---

### T4: Flag `-account` e Bootstrap por conta

**What**: `-account nome=dir` repetível (`flag.Func`); erro com `-projects` junto; `account.Implicit` sem a flag; `WithAccounts` no `store.New`; Bootstrap em cada `ProjectsDirs()`; `OnUnknown` no `log`.
**Where**: `cmd/claude-fleet/main.go`, `main_test.go`
**Depends on**: T1, T2
**Reuses**: `parseConfig`, `bootstrapMaxAge`
**Requirement**: CONTA-02, CONTA-04

**Done when**:

- [x] Sem `-account`: comportamento e saída de hoje (testes atuais de `parseConfig` passam)
- [x] `-account a=… -projects …` sai com 2 e a mensagem do design
- [x] `-account` inválido sai com 2 com a mensagem do `account.Parse`
- [x] Log na partida lista as contas e quantas sessões o Bootstrap achou em cada uma
- [x] Gate passa: `go test -race -count=1 ./cmd/claude-fleet/` com `TestParseConfigAccountComProjectsFalha` e um caso de `-account` válido

**Feito** (1e8c7d7, b6816a3). Desvio do design: **sem conta implícita**. Sem `-account`, o Store roda sem `WithAccounts` e nenhuma sessão tem conta, exatamente como hoje; `account.Implicit` saiu. Motivo: o `-projects` aceita qualquer caminho, e derivar dele `<dir>/projects` marcaria como "outra" sessões que hoje aparecem normalmente. Com isso, "outra" só existe quando há `-account`. Conferido também com o binário real: eventos de duas contas e de um diretório não declarado dão `p`, `t` e `outra`, sem caminho na API, e o aviso no log sai uma vez por diretório.

**Tests**: unit · **Gate**: quick
**Commit**: `feat(cli): flag -account e descoberta de sessões por conta`

---

### T5: Caso multi-conta no `hook/test.sh` [P]

**What**: Cenário com dois diretórios de configuração temporários: `install-hooks -yes` com `CLAUDE_CONFIG_DIR` em cada um, servidor com dois `-account`, um evento por conta pelo script instalado, e `/api/sessions` com `Account` certo em cada sessão. Mais um evento de um diretório não declarado, que deve dar `"outra"`.
**Where**: `hook/test.sh`
**Depends on**: T3, T4
**Reuses**: a estrutura atual do `test.sh` (sobe o servidor real e manda eventos pelo script)
**Requirement**: CONTA-01, CONTA-03

**Done when**:

- [x] Cenário novo passa e os cenários antigos continuam passando
- [x] O `HOME` real não é tocado (tudo em `mktemp -d`)
- [x] Gate full passa

**Feito** (a3a6bcc). O caso 6 também confere que a API não expõe o diretório da conta e que o `install-hooks` com `CLAUDE_CONFIG_DIR` não cria `~/.claude` no `HOME` temporário.

**Tests**: integration · **Gate**: full
**Commit**: `test(hook): sessões de duas contas pelo hook instalado`

---

### T6: Chip da conta no tile e conta no pop-up

**What**: `span.conta` na linha do id quando `s.Account` não é vazio; conta no `detSub` do pop-up; tokens `--conta-1…4` em `:root`; cor por `hash(nome) % 4`; chave de ocultação prefixada com a conta.
**Where**: `internal/web/static/index.html`
**Depends on**: T2
**Reuses**: tokens de `:root`, `el()`, chave de ocultação (`index.html:923`)
**Requirement**: CONTA-06
**Tools**: agente `ui-figma`

**Done when**:

- [x] Com uma conta só (sem `Account`), a página fica pixel a pixel como hoje
- [x] Com duas contas, cada tile mostra o nome da conta, legível em 160 px e com contraste AA nos dois tons
- [x] Sessão já oculta antes da feature continua oculta; ocultar uma sessão de uma conta não oculta a mesma pasta na outra
- [x] Sem fonte, CDN nem dependência externa; respeita `prefers-reduced-motion`
- [x] Conferido no navegador: desktop e celular, vertical e horizontal

**Feito** pelo `ui-figma`. Desvio: no tile de 160 px (abaixo de 600 px) não cabem chip, id e custo; com chip, o id sai do tile e continua no pop-up. Hash djb2 aditivo (na variante XOR, "pessoal" e "trabalho" caíam na mesma cor). Tokens: `--conta-1…4` (azul, violeta, rosa, ciano; contraste 7,96–8,67:1) e `--conta-outra` (6,74:1); sem verde e amarelo, para não confundir com ativo e aguardando. Conferido com Playwright headless em 1280×800, 390×844 e 844×390, com e sem `-account`, sem erro no console; sem `-account` a página e a chave de ocultação ficam como antes.

**Tests**: browser · **Gate**: browser + quick
**Commit**: `feat(ui): conta da sessão no tile e no pop-up`

---

### T7: Figma do chip e do pop-up

**What**: Componente do chip da conta, tile com chip (duas contas) e pop-up com a conta no arquivo "claude-fleet — painel v2", espelhando o T6.
**Where**: Figma (arquivo `tVFsjlauIBbPrj2HSXNron`)
**Depends on**: T6
**Requirement**: CONTA-06
**Tools**: agente `ui-figma` (MCP do Figma)

**Done when**:

- [ ] Tokens `--conta-1…4` como variáveis no Figma
- [ ] Telas de celular (vertical) e desktop atualizadas, com nomes fictícios ("pessoal", "trabalho")
- [ ] Screenshot do Figma comparado com o do navegador

**Tests**: none (artefato de design) · **Gate**: comparação visual
**Commit**: nenhum no repositório (link no PR)

---

### T8: Filtro da grade por conta (P2)

**What**: `<select>` no cabeçalho, só com mais de uma conta nos tiles; escolha em `localStorage` (`fleet.conta`, com try/catch); só visual, sem afetar som e push. Figma junto, pelo `ui-figma`.
**Where**: `internal/web/static/index.html`; Figma
**Depends on**: T7
**Reuses**: padrão de `localStorage` do `ocultar-sessao`; lógica de som/push existente, sem mudança
**Requirement**: CONTA-08
**Tools**: agente `ui-figma`

**Done when**:

- [ ] Filtro escondido com uma conta; aparece com duas
- [ ] Recarregar mantém a escolha; `localStorage` bloqueado volta a "todas" sem erro no console
- [ ] Sessão de conta filtrada que vai para "aguardando" ainda toca o som
- [ ] Cabeçalho cabe no celular, na vertical e na horizontal; Figma atualizado

**Tests**: browser · **Gate**: browser + quick
**Commit**: `feat(ui): filtro da grade por conta`

---

### T9: Deploy e README [P]

**What**: Exemplo de `-account` como drop-in em `deploy/` (comentário na unit) e seção curta no README: aliases com `CLAUDE_CONFIG_DIR`, `install-hooks` por conta e as flags do serviço.
**Where**: `deploy/claude-fleet.service`, `README.md`
**Depends on**: T4
**Requirement**: CONTA-01, CONTA-02

**Done when**:

- [x] Exemplo usa nomes fictícios e `%h` em vez de caminho pessoal
- [x] `systemd-analyze --user verify deploy/claude-fleet.service` sem erro novo

**Feito** (bfe4241). Desvio: em vez de só um comentário, a unit repassa `$CLAUDE_FLEET_ARGS` do `env` ao servidor, e as contas entram ali sem editar o `ExecStart`. `systemd-analyze --user verify` sem erro novo.

**Tests**: none (documentação) · **Gate**: `systemd-analyze` + leitura
**Commit**: `docs: várias contas do Claude Code no README e na unit`

---

### T10: `uso-plano` por conta [P]

**What**: Emendar `features/uso-plano/spec.md`: o edge case "valores são da conta, iguais entre sessões" passa a valer por conta, o arquivo de estado vira mapa por conta e a tela mostra um par de medidores por conta (CONTA-07).
**Where**: `.specs/features/uso-plano/spec.md`
**Depends on**: None
**Requirement**: CONTA-07

**Done when**:

- [x] Critérios do `uso-plano` citam `account.List.Resolve` e o formato do design do multi-conta
- [x] Nenhuma contradição entre as duas specs

**Feito** (7fa0ead).

**Tests**: none (spec) · **Gate**: leitura
**Commit**: `docs(specs): uso-plano guarda o plano por conta`

---

### T11: UAT e fechamento

**What**: `uat.md` do multi-conta com duas contas reais abertas ao mesmo tempo; traceability para Verified; ROADMAP e STATE atualizados; revisão pelo agente `reviewer`.
**Where**: `.specs/features/multi-conta/uat.md`, `spec.md`, `.specs/project/ROADMAP.md`, `STATE.md`
**Depends on**: T5, T7, T8, T9, T10
**Requirement**: todos

**Done when**:

- [ ] UAT: sessões das duas contas com chip certo, inclusive uma aberta antes do serviço subir; filtro funcionando no celular
- [ ] `reviewer` sem achado aberto
- [ ] Gate full passa

**Tests**: UAT · **Gate**: full
**Commit**: `docs(specs): UAT do multi-conta`

---

## Granularity Check

| Task | Scope | Status |
| --- | --- | --- |
| T1 | 1 pacote-folha (1 arquivo + teste) | ✅ |
| T2 | 1 campo em 3 arquivos coesos do `store` | ⚠️ OK, coeso |
| T3 | 1 função (`parseHooksFlags`) + mensagem | ✅ |
| T4 | 1 flag + laço do Bootstrap em `main.go` | ✅ |
| T5 | 1 cenário de teste | ✅ |
| T6 | Chip + subtítulo + chave no mesmo arquivo | ⚠️ OK, coeso |
| T7 | 1 artefato do Figma | ✅ |
| T8 | 1 controle | ✅ |
| T9 | 2 arquivos de documentação | ⚠️ OK, mesma mudança |
| T10 | 1 spec | ✅ |
| T11 | Fechamento | ✅ |

## Diagram-Definition Cross-Check

| Task | Depends On (body) | Diagram Shows | Status |
| --- | --- | --- | --- |
| T1 | None | Phase 1, sem seta | ✅ |
| T2 | T1 | T1 → T2 | ✅ |
| T3 | None | Phase 1, sem seta | ✅ |
| T4 | T1, T2 | T2 → T4 (T1 via T2) | ✅ |
| T5 | T3, T4 | T3, T4 → T5 | ✅ |
| T6 | T2 | T2 → T6 | ✅ |
| T7 | T6 | T6 → T7 | ✅ |
| T8 | T7 | T7 → T8 | ✅ |
| T9 | T4 | T4 → T9 | ✅ |
| T10 | None | Phase 1, sem seta | ✅ |
| T11 | T5, T7, T8, T9, T10 | T5, T7, T8, T9, T10 → T11 | ✅ |

## Test Co-location Validation

| Task | Camada | Exigido | Task diz | Status |
| --- | --- | --- | --- | --- |
| T1 | pacote Go | unit | unit | ✅ |
| T2 | pacote Go | unit | unit | ✅ |
| T3 | `cmd` | unit | unit | ✅ |
| T4 | `cmd` | unit | unit | ✅ |
| T5 | hook | integration (`test.sh`) | integration | ✅ |
| T6 | frontend | navegador (`CLAUDE.md`) | browser | ✅ |
| T7 | Figma | none | none | ✅ |
| T8 | frontend | navegador | browser | ✅ |
| T9 | docs/deploy | none | none | ✅ |
| T10 | spec | none | none | ✅ |
| T11 | UAT | full | full | ✅ |

## Tools

| Tasks | Ferramenta |
| --- | --- |
| T1–T5, T9–T10 | Nenhum MCP; Go e shell |
| T6–T8 | Agente `ui-figma` (código + Figma pelo MCP do Figma) |
| T11 | Agente `reviewer` |
