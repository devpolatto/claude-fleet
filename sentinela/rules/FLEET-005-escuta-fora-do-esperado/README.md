# FLEET-005: claude-fleet escutando fora do loopback e da tailnet

**Severidade:** alta · **Origem:** `deploy/claude-fleet.service` (o `sh -c` sai com erro em vez de cair em `:7777`) e o padrão do `docker-local`

## Problema

O painel deve escutar só em dois lugares: `127.0.0.1` (o hook) e o IP da tailnet (o celular). Um `-addr :7777`, uma unit velha ou um teste manual deixam o painel aberto em todas as interfaces, inclusive na LAN de casa e em Wi-Fi público.

## Por que importa

Na LAN, qualquer aparelho chega no `/login` e nas rotas de leitura. O token continua valendo, mas a superfície deixa de ser só a tailnet, que é autenticada pelo Tailscale.

## Remediação

Suba pelo `deploy/claude-fleet.service`, que resolve o IP com `tailscale ip -4` e se recusa a subir sem ele. Para teste local, use `-addr 127.0.0.1:7777`.

## O que o check verifica

Os listeners TCP do processo `claude-fleet` (`ss -ltnpH`). Cada endereço precisa ser de loopback ou de uma faixa do Tailscale (`100.64.0.0/10`, `fd7a:115c:a1e0::/48`). Também acusa quando o processo não aparece, porque um serviço parado não deve passar como limpo.
