# FLEET-004: segredo do claude-fleet com permissão aberta

**Severidade:** média · **Origem:** padrão positivo registrado na auditoria (estado gravado com 0600 e escrita atômica)

## Problema

`~/.config/claude-fleet/` guarda o token de acesso (`env`), a chave TLS (`tls.key`), a chave VAPID do push (`vapid.pem`), as assinaturas de push e o uso do plano. O servidor grava com 0600, mas uma cópia manual (`cp env env.bak`), um `chmod` ou uma restauração de backup podem abrir as permissões.

## Por que importa

Outro usuário local com o token lê o painel. Com a chave VAPID, manda push falso para o celular do dono.

## Remediação

```sh
chmod 600 ~/.config/claude-fleet/*  && chmod 644 ~/.config/claude-fleet/tls.crt
chmod 700 ~/.config/claude-fleet
```

Cópias do `env` (`env.bak-*`) também carregam o token: apague depois de conferir.

## O que o check verifica

- Todo arquivo da pasta, exceto `tls.crt`, sem bit de grupo nem de outros.
- A pasta sem escrita para grupo ou outros.
- Cópias do `env` (`env.bak*`, `env.orig`, `env.old`, `env~`).

O check só lê as permissões (`stat`), nunca o conteúdo.
