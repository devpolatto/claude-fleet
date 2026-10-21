# FLEET-003: hook ou unit instalados divergem do repo

**Severidade:** média · **Origem:** mesma lição do FLEET-002, aplicada ao que o `install-hooks` e o `deploy/` copiam

## Problema

O `claude-fleet install-hooks` copia `fleet-hook.sh` e `fleet-statusline.sh` para `~/.claude*/claude-fleet/`, e a unit do systemd é copiada de `deploy/`. Uma correção nesses arquivos (o `curl -q --noproxy '*'`, a escolha do endereço do listener) só vale depois de reinstalar.

## Por que importa

- Uma cópia velha do hook pode ler o `~/.curlrc` ou mandar o evento por um proxy.
- Uma unit velha pode subir o servidor em `:7777`, em todas as interfaces (veja também o FLEET-005).

## Remediação

- Hooks: `claude-fleet install-hooks` em cada conta.
- Unit: copie `deploy/claude-fleet.service` para `~/.config/systemd/user/` e rode `systemctl --user daemon-reload && systemctl --user restart claude-fleet`.
- Script instalado que não existe mais no repo: apague, depois de conferir que nenhum settings o chama.

## O que o check verifica

- Cada `*.sh` em `<conta>/claude-fleet/`, nas três `~/.claude*`, comparado byte a byte com `hook/<nome>` do repo.
- A unit da option `unit`, comparada com `deploy/<nome>`.
