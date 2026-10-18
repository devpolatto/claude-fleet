#!/usr/bin/env bash
# Teste de integração dos scripts (gate da T12): `bash hook/test.sh`.
#
# Sobe o servidor real numa porta livre e verifica o hook em seis casos:
#   1. servidor ligado: o evento chega e a sessão fica "ativo";
#   2. servidor desligado: termina em ≤ 1,1 s;
#   3. servidor que aceita e nunca responde: termina em ≤ 1,1 s (valida o -m 1);
#   4. porta inválida em CLAUDE_FLEET_PORT;
#   5. stdin vazio;
#   6. duas contas (CLAUDE_CONFIG_DIR): install-hooks em cada uma, servidor
#      com -account e a conta certa em cada sessão, mais "outra" para um
#      diretório não declarado.
# E o fleet-statusline.sh em mais quatro:
#   7. servidor desligado: termina em ≤ 1,1 s;
#   8. servidor que aceita e nunca responde: termina em ≤ 1,1 s;
#   9. porta inválida em CLAUDE_FLEET_PORT;
#  10. stdin vazio.
# E o uso do plano de ponta a ponta:
#  11. duas contas: install-hooks em cada uma registra o statusLine, o script
#      instalado (lido do settings.json) leva o JSON do status line de cada
#      conta ao servidor, /api/uso mostra o percentual certo por conta, um
#      transcript fora das contas não aparece, e o uso-plano.json tem modo
#      600 e nenhum caminho, session_id nem cwd.
# Nos casos 1 a 5 e 7 a 11: exit 0 e stdout/stderr vazios.
set -u

repo=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
hook="$repo/hook/fleet-hook.sh"
statusline="$repo/hook/fleet-statusline.sh"
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

# run_hook <porta> <payload> [script]: roda o hook (por padrão o do
# repositório) e deixa em $rc, $out, $err e $elapsed.
run_hook() {
	local start end
	start=$(now_ms)
	printf '%s' "$2" | CLAUDE_FLEET_PORT=$1 "${3:-$hook}" >"$tmp/out" 2>"$tmp/err"
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

# payload_sl: JSON no formato do status line, com o uso do plano.
payload_sl() {
	printf '{"session_id":"%s","transcript_path":"%s/x.jsonl","model":{"id":"claude-opus-4-1"},"rate_limits":{"five_hour":{"used_percentage":42,"resets_at":1760000000},"seven_day":{"used_percentage":7,"resets_at":1760500000}}}' "$1" "$tmp"
}

payload() {
	printf '{"hook_event_name":"UserPromptSubmit","session_id":"%s","cwd":"%s","prompt":"teste"}' "$1" "$tmp"
}

# payload_tp <session_id> <transcript_path>: SessionStart com transcript.
payload_tp() {
	printf '{"hook_event_name":"SessionStart","session_id":"%s","cwd":"%s","transcript_path":"%s","source":"startup"}' "$1" "$tmp" "$2"
}

# wait_ready <porta> <pid> <log>: espera o servidor responder.
wait_ready() {
	for _ in $(seq 1 100); do
		curl -q -s -f --noproxy '*' -o /dev/null "http://127.0.0.1:$1/api/sessions" && return 0
		kill -0 "$2" 2>/dev/null || break
		sleep 0.1
	done
	echo "FAIL  servidor não ficou pronto na porta $1"
	cat "$3"
	return 1
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

wait_ready "$port" "$server_pid" "$tmp/server.log" || exit 1
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

# Caso 6: duas contas. HOME aponta para o temporário: nada fora dele muda.
case6="6 duas contas"
home6="$tmp/home"
mkdir -p "$home6"
installed=1
for c in pessoal trabalho; do
	if ! HOME="$home6" CLAUDE_CONFIG_DIR="$home6/.claude-$c" "$tmp/claude-fleet" install-hooks -yes >"$tmp/install-$c.log" 2>&1; then
		fail "$case6" "install-hooks na conta $c falhou: $(cat "$tmp/install-$c.log")"
		installed=""
	elif ! grep -q "$home6/.claude-$c/claude-fleet/fleet-hook.sh" "$home6/.claude-$c/settings.json"; then
		fail "$case6" "settings.json da conta $c sem o hook"
		installed=""
	fi
done
if [ -e "$home6/.claude" ]; then
	fail "$case6" "install-hooks com CLAUDE_CONFIG_DIR criou $home6/.claude"
	installed=""
fi
if [ -n "$installed" ]; then
	port6=$("$tmp/helper-bin" freeport)
	"$tmp/claude-fleet" -addr "127.0.0.1:$port6" -state-dir "$tmp/state6" \
		-account "pessoal=$home6/.claude-pessoal" -account "trabalho=$home6/.claude-trabalho" \
		>"$tmp/server6.log" 2>&1 &
	server_pid=$!
	if wait_ready "$port6" "$server_pid" "$tmp/server6.log"; then
		ok6=1
		for c in pessoal trabalho naodeclarada; do
			sid6="hook-test-$c-$$"
			script="$home6/.claude-$c/claude-fleet/fleet-hook.sh"
			[ -x "$script" ] || script="$hook"
			want="$c"
			[ "$c" = naodeclarada ] && want="outra"
			run_hook "$port6" "$(payload_tp "$sid6" "$home6/.claude-$c/projects/-tmp-proj/$sid6.jsonl")" "$script"
			check_quiet "$case6 ($c)" || { ok6=""; continue; }
			body=$(curl -q -s --noproxy '*' "http://127.0.0.1:$port6/api/session/$sid6")
			if ! printf '%s' "$body" | grep -q "\"Account\":\"$want\""; then
				fail "$case6" "sessão da conta $c sem Account \"$want\"; resposta: $body"
				ok6=""
			elif printf '%s' "$body" | grep -q "$home6"; then
				fail "$case6" "a API expôs o diretório da conta $c"
				ok6=""
			fi
		done
		[ -n "$ok6" ] && pass "$case6 (pessoal, trabalho e outra)"
	else
		failures=$((failures + 1))
	fi
	kill "$server_pid" 2>/dev/null
	wait "$server_pid" 2>/dev/null
	server_pid=""
fi

# Casos 7 a 10: fleet-statusline.sh, com o mesmo roteiro dos casos 2 a 5. A
# porta do caso 1 segue sem ninguém escutando (o servidor do caso 6 usa outra).
# O caso "servidor ligado" do statusline é o 11, com duas contas.
case7="7 statusline: servidor desligado"
run_hook "$port" "$(payload_sl "$sid")" "$statusline"
check_quiet "$case7" && check_fast "$case7" && pass "$case7 (${elapsed} ms)"

case8="8 statusline: servidor mudo"
if [ -z "$hole_port" ]; then
	echo "AVISO caso 8 pulado: não foi possível subir o listener mudo"
else
	run_hook "$hole_port" "$(payload_sl "$sid")" "$statusline"
	check_quiet "$case8" && check_fast "$case8" && pass "$case8 (${elapsed} ms)"
fi

case9="9 statusline: porta inválida"
run_hook "abc" "$(payload_sl "$sid")" "$statusline"
check_quiet "$case9" && check_fast "$case9" && pass "$case9 (${elapsed} ms)"

case10="10 statusline: stdin vazio"
run_hook "${hole_port:-$port}" "" "$statusline"
check_quiet "$case10" && check_fast "$case10" && pass "$case10 (${elapsed} ms)"

# Caso 11: uso do plano de duas contas pelo status line instalado. HOME e as
# contas ficam no temporário, como no caso 6.
case11="11 uso do plano de duas contas"
home11="$tmp/home11"
state11="$tmp/state11"
mkdir -p "$home11"
ok11=1

# payload_sl11 <session_id> <transcript_path> <5h> <7d>: status line com cwd.
payload_sl11() {
	printf '{"session_id":"%s","transcript_path":"%s","cwd":"%s","model":{"id":"claude-opus-4-1"},"rate_limits":{"five_hour":{"used_percentage":%s,"resets_at":1760000000},"seven_day":{"used_percentage":%s,"resets_at":1760500000}}}' "$1" "$2" "$tmp/proj11" "$3" "$4"
}

# statusline_cmd <settings.json>: o statusLine.command registrado.
statusline_cmd() {
	python3 -c 'import json, sys
s = json.load(open(sys.argv[1]))
print((s.get("statusLine") or {}).get("command", ""))' "$1" 2>/dev/null
}

for c in pessoal trabalho; do
	dir="$home11/.claude-$c"
	if ! HOME="$home11" CLAUDE_CONFIG_DIR="$dir" "$tmp/claude-fleet" install-hooks -yes >"$tmp/install11-$c.log" 2>&1; then
		fail "$case11" "install-hooks na conta $c falhou: $(cat "$tmp/install11-$c.log")"
		ok11=""
		continue
	fi
	cmd=$(statusline_cmd "$dir/settings.json")
	if [ "$cmd" != "$dir/claude-fleet/fleet-statusline.sh" ]; then
		fail "$case11" "statusLine.command da conta $c é \"$cmd\", esperado $dir/claude-fleet/fleet-statusline.sh"
		ok11=""
	elif [ ! -x "$cmd" ]; then
		fail "$case11" "script do statusLine da conta $c não é executável: $cmd"
		ok11=""
	fi
done

if [ -n "$ok11" ]; then
	port11=$("$tmp/helper-bin" freeport)
	"$tmp/claude-fleet" -addr "127.0.0.1:$port11" -state-dir "$state11" \
		-account "pessoal=$home11/.claude-pessoal" -account "trabalho=$home11/.claude-trabalho" \
		>"$tmp/server11.log" 2>&1 &
	server_pid=$!
	if wait_ready "$port11" "$server_pid" "$tmp/server11.log"; then
		# conta  5h  7d: percentuais distintos por conta; "fora" usa 99/98, e
		# o Python abaixo garante que eles não caíram em conta nenhuma.
		for spec in "pessoal 42 7" "trabalho 63.5 21" "fora 99 98"; do
			set -- $spec
			c=$1
			sid11="sl-test-$c-$$"
			if [ "$c" = fora ]; then
				tp="$tmp/fora/projects/-tmp-proj11/$sid11.jsonl"
				script=$(statusline_cmd "$home11/.claude-pessoal/settings.json")
			else
				tp="$home11/.claude-$c/projects/-tmp-proj11/$sid11.jsonl"
				script=$(statusline_cmd "$home11/.claude-$c/settings.json")
			fi
			run_hook "$port11" "$(payload_sl11 "$sid11" "$tp" "$2" "$3")" "$script"
			check_quiet "$case11 ($c)" || ok11=""
		done

		body=$(curl -q -s --noproxy '*' "http://127.0.0.1:$port11/api/uso")
		if ! problem=$(printf '%s' "$body" | python3 -c '
import json, sys
want = {"pessoal": (42, 7), "trabalho": (63.5, 21)}
got = {e.get("account"): e for e in json.load(sys.stdin)}
for name, (h5, d7) in want.items():
    e = got.get(name)
    if e is None:
        sys.exit("conta %s ausente" % name)
    pair = ((e.get("five_hour") or {}).get("used_percentage"),
            (e.get("seven_day") or {}).get("used_percentage"))
    if pair != (h5, d7):
        sys.exit("conta %s com %s, esperado %s" % (name, pair, (h5, d7)))
for name, e in got.items():
    if name not in want and (e.get("five_hour") or e.get("seven_day")):
        sys.exit("conta %r recebeu o envio de fora das contas" % name)
' 2>&1); then
			fail "$case11" "/api/uso: $problem; resposta: $body"
			ok11=""
		elif printf '%s' "$body" | grep -q 'transcript_path\|session_id\|"cwd"\|sl-test-'; then
			fail "$case11" "/api/uso com dado da sessão: $body"
			ok11=""
		fi

		f11="$state11/uso-plano.json"
		if [ ! -f "$f11" ]; then
			fail "$case11" "$f11 não existe"
			ok11=""
		else
			mode=$(stat -c %a "$f11" 2>/dev/null || stat -f %Lp "$f11")
			if [ "$mode" != 600 ]; then
				fail "$case11" "uso-plano.json com modo $mode (esperado 600)"
				ok11=""
			fi
			for bad in transcript_path session_id '"cwd"' "$home11" "$tmp/fora" "$tmp/proj11" sl-test-; do
				if grep -qF -- "$bad" "$f11"; then
					fail "$case11" "uso-plano.json contém $bad"
					ok11=""
				fi
			done
		fi
	else
		failures=$((failures + 1))
		ok11=""
	fi
	kill "$server_pid" 2>/dev/null
	wait "$server_pid" 2>/dev/null
	server_pid=""
fi
[ -n "$ok11" ] && pass "$case11 (pessoal 42/7, trabalho 63.5/21, fora descartado)"

if [ "$failures" -ne 0 ]; then
	echo "$failures caso(s) falharam"
	exit 1
fi
echo "Todos os casos passaram"
