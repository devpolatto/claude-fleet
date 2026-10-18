#!/bin/sh
# Status line do claude-fleet: repassa o JSON do status line (stdin) ao
# servidor local, que extrai só o uso do plano (rate_limits).
#
# Regras (USO-01, AD-006), as mesmas do fleet-hook.sh:
# - síncrono, com teto de 1 s (curl -m 1); nada de `async`;
# - stdout e stderr sempre vazios: o Claude Code mostra o stdout como a
#   linha de status, e o painel não deve aparecer nela;
# - sempre exit 0, mesmo com servidor fora, curl ausente ou porta inválida;
# - o payload não é gravado em disco nem em log.

port=${CLAUDE_FLEET_PORT:-7777}

# Porta só com dígitos; qualquer outra coisa desiste em silêncio.
case $port in
	'' | *[!0-9]*) exit 0 ;;
esac

# Sem curl não há o que fazer.
command -v curl >/dev/null 2>&1 || exit 0

# Stdin num terminal (execução manual) não é payload do status line; não espera.
[ -t 0 ] && exit 0

# -q ignora o ~/.curlrc (precisa ser o primeiro argumento) e --noproxy evita
# que um http_proxy do ambiente desvie o envio para fora do loopback.
curl -q -s -m 1 --noproxy '*' --data-binary @- \
	-H 'Content-Type: application/json' \
	"http://127.0.0.1:${port}/statusline" >/dev/null 2>&1

exit 0
