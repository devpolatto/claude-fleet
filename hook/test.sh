#!/usr/bin/env bash
# Teste de integração do hook (gate da T12): `bash hook/test.sh`.
#
# Sobe o servidor real numa porta livre e verifica o hook em cinco casos:
#   1. servidor ligado: o evento chega e a sessão fica "ativo";
#   2. servidor desligado: termina em ≤ 1,1 s;
#   3. servidor que aceita e nunca responde: termina em ≤ 1,1 s (valida o -m 1);
#   4. porta inválida em CLAUDE_FLEET_PORT;
#   5. stdin vazio.
# Em todos: exit 0 e stdout/stderr vazios.
set -u

repo=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
hook="$repo/hook/fleet-hook.sh"
limit_ms=1100

tmp=$(mktemp -d "${TMPDIR:-/tmp}/fleet-hook-test.XXXXXX")
server_pid=""
hole_pid=""

cleanup() {
	for pid in "$server_pid" "$hole_pid"; do
		[ -n "$pid" ] && kill "$pid" 2>/dev/null && wait "$pid" 2>/dev/null
	done
	rm -rf "$tmp"
}
trap cleanup EXIT
trap 'exit 130' INT TERM

failures=0
pass() { printf 'PASS  %s\n' "$1"; }
fail() {
	printf 'FAIL  %s: %s\n' "$1" "$2"
	failures=$((failures + 1))
}

# Relógio em milissegundos; $EPOCHREALTIME (bash ≥ 5) evita o `date +%N`,
# que não existe no macOS.
now_ms() {
	if [ -n "${EPOCHREALTIME:-}" ]; then
		local t=${EPOCHREALTIME/[,.]/}
		echo $((10#$t / 1000))
	else
		python3 -c 'import time; print(int(time.time() * 1000))'
	fi
}

# run_hook <porta> <payload>: roda o hook e deixa em $rc, $out, $err e $elapsed.
run_hook() {
	local start end
	start=$(now_ms)
	printf '%s' "$2" | CLAUDE_FLEET_PORT=$1 "$hook" >"$tmp/out" 2>"$tmp/err"
	rc=$?
	end=$(now_ms)
	elapsed=$((end - start))
	out=$(cat "$tmp/out")
	err=$(cat "$tmp/err")
}

# check_quiet <caso>: exit 0 e saídas vazias. Devolve 1 se falhou.
check_quiet() {
	if [ "$rc" -ne 0 ]; then
		fail "$1" "exit $rc"
		return 1
	fi
	if [ -n "$out" ] || [ -n "$err" ]; then
		fail "$1" "saída não vazia (stdout=${#out} B, stderr=${#err} B)"
		return 1
	fi
}

check_fast() {
	if [ "$elapsed" -gt "$limit_ms" ]; then
		fail "$1" "levou ${elapsed} ms (limite ${limit_ms} ms)"
		return 1
	fi
}

payload() {
	printf '{"hook_event_name":"UserPromptSubmit","session_id":"%s","cwd":"%s","prompt":"teste"}' "$1" "$tmp"
}

command -v go >/dev/null || { echo "FAIL  go não encontrado"; exit 1; }
command -v curl >/dev/null || { echo "FAIL  curl não encontrado"; exit 1; }

# Programa auxiliar em Go (já é dependência do build), portável entre Linux e
# macOS, ao contrário das variantes de `nc -l`:
#   freeport  imprime uma porta TCP livre em 127.0.0.1;
#   blackhole escuta, imprime a porta e aceita conexões sem nunca responder.
mkdir -p "$tmp/helper"
cat >"$tmp/helper/main.go" <<'GO'
package main

import (
	"fmt"
	"net"
	"os"
)

func main() {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		os.Exit(1)
	}
	fmt.Println(ln.Addr().(*net.TCPAddr).Port)
	if len(os.Args) > 1 && os.Args[1] == "blackhole" {
		var held []net.Conn
		for {
			c, err := ln.Accept()
			if err != nil {
				os.Exit(1)
			}
			held = append(held, c)
		}
	}
	ln.Close()
}
GO

echo "Compilando servidor e auxiliar..."
if ! (cd "$repo" && go build -o "$tmp/claude-fleet" ./cmd/claude-fleet); then
	echo "FAIL  build do servidor"
	exit 1
fi
if ! (cd "$tmp/helper" && GO111MODULE=off go build -o "$tmp/helper-bin" main.go); then
	echo "FAIL  build do auxiliar"
	exit 1
fi

port=$("$tmp/helper-bin" freeport)
mkdir -p "$tmp/projects"
"$tmp/claude-fleet" -addr "127.0.0.1:$port" -projects "$tmp/projects" >"$tmp/server.log" 2>&1 &
server_pid=$!

ready=""
for _ in $(seq 1 100); do
	if curl -q -s -f --noproxy '*' -o /dev/null "http://127.0.0.1:$port/api/sessions"; then
		ready=1
		break
	fi
	kill -0 "$server_pid" 2>/dev/null || break
	sleep 0.1
done
if [ -z "$ready" ]; then
	echo "FAIL  servidor não ficou pronto na porta $port"
	cat "$tmp/server.log"
	exit 1
fi
echo "Servidor pronto em 127.0.0.1:$port"

# Caso 1: servidor ligado.
case1="1 servidor ligado"
sid="hook-test-$$-$(now_ms)"
run_hook "$port" "$(payload "$sid")"
if check_quiet "$case1"; then
	body=$(curl -q -s --noproxy '*' "http://127.0.0.1:$port/api/session/$sid")
	if printf '%s' "$body" | grep -q '"Status":"ativo"'; then
		pass "$case1 (${elapsed} ms, sessão $sid ativa)"
	else
		fail "$case1" "sessão não ficou ativa; resposta: $body"
	fi
fi

# Caso 2: servidor desligado (mesma porta, agora sem ninguém escutando).
kill "$server_pid" 2>/dev/null
wait "$server_pid" 2>/dev/null
server_pid=""
case2="2 servidor desligado"
run_hook "$port" "$(payload "$sid")"
check_quiet "$case2" && check_fast "$case2" && pass "$case2 (${elapsed} ms)"

# Caso 3: servidor que aceita a conexão e nunca responde.
case3="3 servidor mudo"
"$tmp/helper-bin" blackhole >"$tmp/hole.port" &
hole_pid=$!
hole_port=""
for _ in $(seq 1 50); do
	hole_port=$(cat "$tmp/hole.port" 2>/dev/null)
	[ -n "$hole_port" ] && break
	sleep 0.1
done
if [ -z "$hole_port" ]; then
	echo "AVISO caso 3 pulado: não foi possível subir o listener mudo"
else
	run_hook "$hole_port" "$(payload "$sid")"
	check_quiet "$case3" && check_fast "$case3" && pass "$case3 (${elapsed} ms)"
fi

# Caso 4: porta inválida.
case4="4 porta inválida"
run_hook "abc" "$(payload "$sid")"
check_quiet "$case4" && check_fast "$case4" && pass "$case4 (${elapsed} ms)"

# Caso 5: stdin vazio (contra o listener mudo, o pior cenário de espera).
case5="5 stdin vazio"
run_hook "${hole_port:-$port}" ""
check_quiet "$case5" && check_fast "$case5" && pass "$case5 (${elapsed} ms)"

if [ "$failures" -ne 0 ]; then
	echo "$failures caso(s) falharam"
	exit 1
fi
echo "Todos os casos passaram"
