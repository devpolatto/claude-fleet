# claude-fleet

Painel web, feito para o celular, que mostra ao vivo as sessões do Claude Code da máquina e os subagentes de cada uma. Visão e escopo em [`.specs/project/PROJECT.md`](.specs/project/PROJECT.md); decisões (AD-xxx) e lições (L-xxx) em [`.specs/project/STATE.md`](.specs/project/STATE.md). Leia o `STATE.md` antes de mudar comportamento: muita coisa que parece arbitrária tem uma decisão por trás.

## Layout

| Caminho | O que faz |
| --- | --- |
| `cmd/claude-fleet` | `main`: flags, listeners (loopback + tailnet), `install-hooks`/`uninstall-hooks` |
| `hook/` | `fleet-hook.sh` (única fonte do script, embutido no binário) e `test.sh` (integração) |
| `internal/ingest` | Decodifica o evento do hook numa lista branca de metadados; o resto é descartado |
| `internal/store` | Estado em memória das sessões e subagentes; `Refresher` relê os transcripts a cada 5 s |
| `internal/transcript` | Leitura incremental dos JSONL de `~/.claude/projects/`, tokens deduplicados por `message.id` |
| `internal/pricing` | Tabela de preço de lista da API e custo estimado |
| `internal/push` | Web Push: VAPID (RFC 8292), cifra aes128gcm (RFC 8291), inscrições em disco |
| `internal/web` | HTTP, SSE, auth por token/cookie, certificado TLS relido do disco |
| `internal/web/static` | Frontend inteiro: `index.html` (HTML + CSS + JS puro), `login.html`, `sw.js`, manifest e ícones, via `embed` |
| `deploy/` | Units systemd de usuário |
| `.specs/` | Planejamento (skill `tlc-spec-driven`): `project/` e uma pasta por feature com `spec.md`, `design.md`, `tasks.md`, `uat.md` |
| `.claude/` | `agents/` (subagentes do projeto) e `skills/tlc-spec-driven` (cópia da skill de planejamento v2.0.0, CC-BY-4.0, origem e licença no `NOTICE`) |

## Comandos

```sh
gofmt -l .                    # precisa sair vazio
go vet ./...
go test -race -count=1 ./...
bash hook/test.sh             # sobe o servidor real e testa o hook
go build -o ~/.local/bin/claude-fleet ./cmd/claude-fleet
```

O CI (`.github/workflows/ci.yml`, job `test`) roda exatamente esses quatro checks.

## Fluxo de trabalho

- **Nada vai direto na `main`.** Ela é protegida por ruleset: só entra por PR, com o check `test` verde, sem force-push. Trabalhe numa branch (`feat/…`, `fix/…`, `chore/…`, `docs/…`) e abra o PR com `gh pr create`.
- Commits em português, Conventional Commits, escopo pelo pacote ou área: `feat(ui)`, `feat(web)`, `fix(store)`, `feat(push)`, `docs(specs)`, `ci`. Um commit por mudança lógica.
- Mensagens de commit e descrições de PR **sem** linha de atribuição a ferramenta (`Co-Authored-By: Claude …`, `Generated with Claude Code`).
- Feature nova começa no `.specs/features/<nome>/spec.md`; o código e a atualização do `STATE.md`/`uat.md` andam no mesmo PR. A skill `tlc-spec-driven` vem com o repositório (`.claude/skills/`) e guia spec, design e tasks; não altere a cópia sem indicar a mudança no `NOTICE` (exigência da CC-BY-4.0). Onde a skill e este arquivo divergirem (idioma e escopo dos commits, por exemplo), vale este arquivo.

## Invariantes (não quebre sem uma AD nova no STATE.md)

- **Só stdlib.** O `go.mod` não tem `require`, e assim deve continuar. Push, VAPID e a cifra são implementados à mão de propósito.
- **Sem conteúdo (AD-003).** O servidor guarda e loga só metadados. Prompt, `tool_input`, `tool_response` e afins nunca podem ser gravados, logados nem enviados. Campo novo no `ingest` precisa de motivo.
- **Somente leitura.** O painel não age sobre as sessões: não aprova, não interrompe, não envia prompt.
- **`POST /event` só do loopback**; as demais rotas exigem token, comparado em tempo constante. Não sugira `tailscale serve` na frente: tudo chegaria como loopback.
- **O hook nunca atrapalha o Claude Code:** sai sempre com 0, não escreve em stdout nem stderr, termina em ≤ 1 s com o servidor fora, e é síncrono (AD-006, sem `async`).
- **Frontend sem build e sem rede externa:** nada de CDN, framework ou fonte remota. Cores e medidas saem dos tokens em `:root` do `index.html`; respeite `prefers-reduced-motion` e o layout de celular (vertical e horizontal).

## Testes

- Nomes em português descrevendo o comportamento (`TestSendAllApagaInscricaoVencida`), em geral com tabela de casos.
- Fixtures reais (anonimizadas) em `testdata/` de cada pacote. Ao capturar um payload ou transcript novo, tire prompts, caminhos pessoais e nomes de máquina antes de versionar: o repositório é público.
- Mudança no hook roda `bash hook/test.sh`; mudança visual é conferida no navegador, no desktop e em largura de celular.

## Subagentes do projeto

Em `.claude/agents/`. A sessão principal planeja e delega; cada agente devolve só a conclusão.

- **`reviewer`**: revisa o diff da branch contra as invariantes acima, sem editar nada. Rode antes de abrir o PR.
- **`ui-figma`**: implementa ou ajusta a interface em `internal/web/static/`, a partir de um design do Figma ou de uma descrição, e também leva telas do painel para o Figma.
