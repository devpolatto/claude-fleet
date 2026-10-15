# claude-fleet

Painel web, feito para o celular, que mostra ao vivo as sessões do Claude Code rodando nesta máquina e os subagentes de cada uma. Cada sessão é um quadradinho que pulsa enquanto trabalha e fica amarelo quando espera permissão. Um toque abre os detalhes: tempo de vida, tokens por categoria e custo estimado.

- **Somente leitura:** o painel não envia prompts, não aprova permissões e não encerra nada.
- **Sem conteúdo:** os hooks repassam o evento inteiro, mas o servidor guarda só metadados (nome do evento, ferramenta, status, ids). Prompts, comandos e respostas são descartados ao chegar.
- **Um binário só**, sem banco e sem dependências; o estado fica em memória e os tokens são relidos dos transcripts em `~/.claude/projects/`.

> **Custo é estimativa.** O valor em US$ é calculado pelo preço de lista da API. Em plano de assinatura ele não corresponde à fatura; serve para comparar sessões entre si.

## Requisitos

- Linux, Go 1.27 ou mais novo, `curl`.
- Claude Code com hooks (testado na 2.1.301).
- Para o celular: [Tailscale](https://tailscale.com) na máquina e no celular, na mesma tailnet.

## 1. Compilar

```sh
git clone https://github.com/devpolatto/claude-fleet.git
cd claude-fleet
go build -o ~/.local/bin/claude-fleet ./cmd/claude-fleet
```

## 2. Instalar os hooks

O instalador grava o script do hook em `~/.claude/claude-fleet/fleet-hook.sh` e acrescenta uma entrada em 16 eventos do `~/.claude/settings.json`, ao lado dos hooks que já existem. Antes de gravar, ele mostra o diff, pede confirmação e faz um backup datado (`settings.json.bak-AAAAMMDD-HHMMSS`).

```sh
claude-fleet install-hooks
```

- Rodar de novo não duplica nada ("nada a fazer").
- Para remover: `claude-fleet uninstall-hooks`. Só as entradas do claude-fleet saem; o script em `~/.claude/claude-fleet/` fica, e pode ser apagado à mão.
- Opções: `-settings <arquivo>` (outro settings), `-dir <diretório>` (onde gravar o script) e `-yes` (sem confirmação).
- Sessões do Claude Code já abertas podem só enxergar os hooks novos depois de abrir `/hooks` ou reiniciar.

<details>
<summary>Instalação manual</summary>

Copie `hook/fleet-hook.sh` para um lugar fixo, dê `chmod 755` e acrescente, em cada evento abaixo dentro de `"hooks"` no `settings.json`, um grupo com o caminho absoluto do script:

```json
{"hooks": [{"type": "command", "command": "/home/voce/.claude/claude-fleet/fleet-hook.sh", "timeout": 5}]}
```

Eventos: `SessionStart`, `SessionEnd`, `UserPromptSubmit`, `PreToolUse`, `PostToolUse`, `PostToolUseFailure`, `PostToolBatch`, `PermissionRequest`, `PermissionDenied`, `Notification`, `Stop`, `StopFailure`, `SubagentStart`, `SubagentStop`, `PreCompact`, `PostCompact`. Não use `matcher` nem `async`: o hook é síncrono de propósito, porque um envio em segundo plano pode se perder quando a sessão termina.

</details>

O hook manda o JSON do evento para `http://127.0.0.1:7777/event` com `curl -m 1` e sempre termina com sucesso, sem escrever nada na saída. Com o servidor fora do ar, ele volta na hora; o pior caso é 1 s, com um servidor travado. Se o servidor usar outra porta, defina `CLAUDE_FLEET_PORT` no ambiente do Claude Code (por exemplo, no bloco `"env"` do `settings.json`).

## 3. Rodar localmente

```sh
claude-fleet
```

Abra <http://127.0.0.1:7777>. Sem flags o servidor escuta só em `127.0.0.1:7777` e não pede token. Ao subir, ele também mostra as sessões cujo transcript mudou nos últimos 30 minutos, como "desconhecido" até o próximo evento delas.

| Flag | Padrão | Uso |
| --- | --- | --- |
| `-addr` | `127.0.0.1:7777` | Endereço de escuta (`host:porta`). |
| `-token` | `$CLAUDE_FLEET_TOKEN` | Obrigatório quando o endereço não é loopback. |
| `-projects` | `~/.claude/projects` | Onde ficam os transcripts. |

Um endereço fora do loopback sem token é recusado: o processo sai com código 2.

## 4. Acessar pelo celular (Tailscale)

1. Gere um token e guarde-o num arquivo só seu:

   ```sh
   mkdir -p ~/.config/claude-fleet
   printf 'CLAUDE_FLEET_TOKEN=%s\n' "$(head -c 32 /dev/urandom | base64 | tr -d '/+=')" > ~/.config/claude-fleet/env
   chmod 600 ~/.config/claude-fleet/env
   ```

2. Suba o servidor no IP da máquina na tailnet:

   ```sh
   set -a; . ~/.config/claude-fleet/env; set +a
   claude-fleet -addr "$(tailscale ip -4):7777"
   ```

   Com um IP específico, o servidor também escuta em `127.0.0.1:7777`, para o hook continuar entregando os eventos.

3. No celular, conectado à tailnet, abra uma vez `http://<IP da tailnet>:7777/?t=<token>`. A página grava um cookie (`HttpOnly`, `SameSite=Strict`, 30 dias) e, daí em diante, basta `http://<IP da tailnet>:7777/`.

Como fica protegido:

- `POST /event` só aceita conexões vindas da própria máquina; pela tailnet ele responde 403.
- As outras rotas exigem o token (cookie ou `?t=`), comparado em tempo constante.
- O tráfego é cifrado pelo Tailscale (WireGuard); o painel em si é HTTP.

> **Não use `tailscale serve` na frente do painel.** Ele faz proxy a partir da própria máquina, então toda requisição chega como loopback, e o `/event` passaria a aceitar eventos falsos vindos da tailnet. Use o bind direto no IP da tailnet, como acima. Também não exponha a porta na internet.

## 5. Deixar sempre de pé (systemd de usuário)

O arquivo [`deploy/claude-fleet.service`](deploy/claude-fleet.service) sobe o painel no IP da tailnet com o token de `~/.config/claude-fleet/env`.

```sh
mkdir -p ~/.config/systemd/user
cp deploy/claude-fleet.service ~/.config/systemd/user/
systemctl --user daemon-reload
systemctl --user enable --now claude-fleet
journalctl --user -u claude-fleet -f   # logs
```

Para o serviço continuar rodando sem sessão aberta: `sudo loginctl enable-linger "$USER"`. Se o Tailscale ainda não tiver IP quando o serviço subir, ele tenta de novo a cada 10 s.

## Como funciona

```
Claude Code ──hook (stdin JSON)──▶ fleet-hook.sh ──curl -m 1──▶ POST /event (só loopback)
                                                                     │ lista branca de metadados
                                                                     ▼
~/.claude/projects/*.jsonl ──leitura incremental a cada 5 s──▶ Store em memória ──SSE──▶ página
```

- O **status** vem dos hooks, na hora: ativo, aguardando permissão, parado, encerrado ou desconhecido (sessão descoberta no boot).
- **Tokens e custo** vêm dos transcripts, com até 5 s de atraso, contando cada mensagem uma vez só.
- Sessões encerradas somem depois de 15 min; sem nenhum evento por 12 h, também.

## Desenvolvimento

```sh
go vet ./... && go test -race ./...   # Go
bash hook/test.sh                     # hook: ligado, desligado, mudo, porta inválida, stdin vazio
```

O planejamento está em [`.specs/`](.specs/): visão em `project/PROJECT.md`, decisões em `project/STATE.md` e spec, design e tasks em `features/fleet-mvp/`.
