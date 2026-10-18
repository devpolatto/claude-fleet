# multi-conta — UAT

**Data:** 2026-10-18 · **Base:** `feat/multi-conta` em `9a7910e` · **Navegador:** celular do usuário, pela tailnet
**Ambiente:** o serviço systemd real da máquina, com o binário da branch e `CLAUDE_FLEET_ARGS` declarando duas contas reais (aqui chamadas de **pessoal** e **trabalho**). Cada conta tem o próprio `CLAUDE_CONFIG_DIR`, aberto por um alias, com o hook instalado por `CLAUDE_CONFIG_DIR=<dir> claude-fleet install-hooks`. Uma terceira sessão, aberta com o `claude` sem alias (`~/.claude`, não declarado), fez o papel de "outra".

Legenda: ✅ verificado · ⚠️ desvio · ➖ não verificado neste UAT

## Resultado

| Critério | Resultado |
| --- | --- |
| CONTA-01: hook instalado em cada conta pelo `CLAUDE_CONFIG_DIR` | ✅ `install-hooks` gravou o hook nos 16 eventos do `settings.json` de cada conta, com backup; o `~/.claude` não foi tocado |
| CONTA-02: `-account` no serviço, via `CLAUDE_FLEET_ARGS` no `env` | ✅ o log da partida lista as duas contas com o diretório de cada uma |
| CONTA-03: conta pelo `transcript_path` | ✅ `/api/sessions` com `pessoal`, `trabalho` e `outra` nas três sessões abertas |
| CONTA-04: sessões já abertas achadas na partida, por conta | ✅ depois de reiniciar o serviço, as duas sessões voltaram com a conta certa, em "desconhecido", sem nenhum evento novo. Ver a observação 1 |
| CONTA-05: a API expõe só o nome da conta | ✅ nenhum diretório no JSON; o log de conta não declarada cita só o diretório de configuração |
| CONTA-06: conta no pop-up; tile sem chip | ✅ no celular |
| CONTA-07: uso do plano por conta | ➖ a feature `uso-plano` ainda não existe; a spec dela foi emendada (T10) |
| CONTA-08: filtro por conta no cabeçalho, mantido ao recarregar | ✅ no celular |
| Som e push de sessão de conta filtrada | ➖ não reportado à parte no UAT; no T8, a chamada de `tocar()` foi conferida com uma sessão de conta filtrada indo para "aguardando" |
| Sessão de diretório não declarado | ✅ aparece como "outra", com um aviso único no log |
| `reviewer` | ✅ nenhum achado alto ou médio; os quatro baixos corrigidos em `267a473`, `7e91164` e `9a7910e` |
| Gate full (`gofmt`, `vet`, `test -race`, `hook/test.sh` com o caso 6) | ✅ |

## Observações

1. **O primeiro reinício achou 0 sessões, e estava certo.** As duas sessões estavam abertas, mas sem nenhum prompt: o Claude Code só cria o transcript no primeiro prompt (os arquivos nasceram 2 s depois do reinício). Sem arquivo, a partida não tem o que achar. Repetido com os transcripts já em disco: 1 sessão por conta.
2. **Sessão de `~/.claude` some no reinício.** Diretório não declarado não entra na busca da partida; a sessão volta como "outra" no evento seguinte. Esperado, e o usuário passou a abrir o Claude Code só pelos aliases.
3. **`CLAUDE_FLEET_ARGS` com espaço no `env`.** O systemd lê a linha inteira, mas um `source` do arquivo num shell falha (`-account: command not found`). Só importa para quem lê o `env` por script; o serviço não é afetado.
