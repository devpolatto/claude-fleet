# Sentinela do claude-fleet

Regras de segurança do claude-fleet **implantado**: o serviço rodando, o binário, os hooks instalados nas contas e a pasta de config. Os testes Go do repo provam o código; estas regras provam que é esse código que está no ar.

Rodar (motor em `~/Playground/sentinela`):

```sh
~/Playground/sentinela/audit ~/Playground/claude-fleet          # regras locais
~/Playground/sentinela/audit ~/Playground/claude-fleet --live   # + sondas contra o serviço
~/Playground/sentinela/audit ~/Playground/claude-fleet --test   # testa as regras
```

| ID | Severidade | O que pega |
|---|---|---|
| [FLEET-001](rules/FLEET-001-sondas-ao-vivo/) | alta | serviço rodando aceita pedido de navegador, rebinding ou cross-site (`--live`) |
| [FLEET-002](rules/FLEET-002-binario-desatualizado/) | alta | binário em `~/.local/bin` compilado de fontes diferentes das do `origin/main` |
| [FLEET-005](rules/FLEET-005-escuta-fora-do-esperado/) | alta | processo escutando fora do loopback e da tailnet |
| [FLEET-003](rules/FLEET-003-instalado-diverge/) | média | hook nas `~/.claude*` ou unit do systemd diferentes dos do repo |
| [FLEET-004](rules/FLEET-004-permissao-de-segredo/) | média | token, chave TLS ou VAPID com permissão aberta; cópia do `env` |

As options de `sentinela.json` (`local_url`, `tailnet_port`, `binary`, `config_dir`, `unit`) apontam para a instalação desta máquina. O IP da tailnet vem de `tailscale ip -4`.
