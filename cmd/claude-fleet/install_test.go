package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/devpolatto/claude-fleet/hook"
)

const fixture = "testdata/settings-atual.json"

var fixedNow = func() time.Time { return time.Date(2026, 10, 15, 14, 30, 5, 0, time.Local) }

// setupSettings copia a fixture para um diretório temporário e devolve o
// caminho do settings e o diretório do script.
func setupSettings(t *testing.T, content []byte) (settings, dir string) {
	t.Helper()
	tmp := t.TempDir()
	settings = filepath.Join(tmp, "settings.json")
	if content != nil {
		if err := os.WriteFile(settings, content, 0o640); err != nil {
			t.Fatal(err)
		}
	}
	return settings, filepath.Join(tmp, "claude-fleet")
}

func readFixture(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func opts(settings, dir, stdin string, yes bool, out io.Writer) hookOpts {
	return hookOpts{settings: settings, dir: dir, yes: yes, in: strings.NewReader(stdin), out: out, now: fixedNow}
}

func mustInstall(t *testing.T, settings, dir string) string {
	t.Helper()
	var out bytes.Buffer
	if err := installHooks(opts(settings, dir, "", true, &out)); err != nil {
		t.Fatalf("install: %v\n%s", err, out.String())
	}
	return out.String()
}

func mustUninstall(t *testing.T, settings string) string {
	t.Helper()
	var out bytes.Buffer
	if err := uninstallHooks(opts(settings, "", "", true, &out)); err != nil {
		t.Fatalf("uninstall: %v\n%s", err, out.String())
	}
	return out.String()
}

func readFile(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func backups(t *testing.T, settings string) []string {
	t.Helper()
	m, err := filepath.Glob(settings + ".bak-*")
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// eventGroups decodifica hooks.<evento> como lista de grupos crus.
func eventGroups(t *testing.T, data []byte) map[string][]json.RawMessage {
	t.Helper()
	var s struct {
		Hooks map[string][]json.RawMessage `json:"hooks"`
	}
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("settings inválido: %v\n%s", err, data)
	}
	return s.Hooks
}

func compact(t *testing.T, raw []byte) string {
	t.Helper()
	var b bytes.Buffer
	if err := json.Compact(&b, raw); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func wantGroup(dir string) string {
	return `{"hooks":[{"type":"command","command":"` + filepath.Join(dir, "fleet-hook.sh") + `","timeout":5}]}`
}

func TestInstallPreservaEntradasEFormato(t *testing.T) {
	orig := readFixture(t)
	settings, dir := setupSettings(t, orig)
	mustInstall(t, settings, dir)
	got := readFile(t, settings)

	// Chaves de topo: mesma ordem e, fora de "hooks", os mesmos bytes.
	before, err := parseObject(orig)
	if err != nil {
		t.Fatal(err)
	}
	after, err := parseObject(got)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != len(after) {
		t.Fatalf("topo tinha %d chaves, ficou com %d", len(before), len(after))
	}
	for i := range before {
		if before[i].key != after[i].key {
			t.Errorf("chave %d: era %q, virou %q", i, before[i].key, after[i].key)
		}
		if before[i].key != "hooks" && !bytes.Equal(before[i].raw, after[i].raw) {
			t.Errorf("valor de %q mudou:\n%s\n---\n%s", before[i].key, before[i].raw, after[i].raw)
		}
	}

	// Os 4 grupos antigos continuam idênticos, na primeira posição, e com os
	// bytes originais.
	oldG, newG := eventGroups(t, orig), eventGroups(t, got)
	for ev, groups := range oldG {
		if len(newG[ev]) != 2 {
			t.Fatalf("%s: queria 2 grupos, veio %d", ev, len(newG[ev]))
		}
		if compact(t, groups[0]) != compact(t, newG[ev][0]) {
			t.Errorf("%s: grupo alheio mudou:\n%s\n---\n%s", ev, groups[0], newG[ev][0])
		}
		if !bytes.Contains(got, groups[0]) {
			t.Errorf("%s: bytes do grupo alheio não foram preservados", ev)
		}
	}

	// Os 16 eventos têm a entrada do claude-fleet, sem matcher nem async.
	if len(newG) != len(fleetEvents) {
		t.Errorf("hooks tem %d eventos, queria %d", len(newG), len(fleetEvents))
	}
	for _, ev := range fleetEvents {
		groups := newG[ev]
		if len(groups) == 0 || compact(t, groups[len(groups)-1]) != wantGroup(dir) {
			t.Errorf("%s sem a entrada do claude-fleet: %s", ev, groups)
		}
	}

	// Linhas fora do bloco "hooks" ficam idênticas.
	ol, nl := splitLines(string(orig)), splitLines(string(got))
	start := indexOf(ol, `  "hooks": {`)
	end := start + indexOf(ol[start:], `  },`) // fecha "hooks"
	if start < 0 || end < start || !strings.HasPrefix(ol[end+1], `  "env"`) {
		t.Fatalf("fixture inesperada: start=%d end=%d", start, end)
	}
	if !reflect.DeepEqual(ol[:start+1], nl[:start+1]) {
		t.Errorf("linhas antes de hooks mudaram:\n%q\n%q", ol[:start+1], nl[:start+1])
	}
	tail := len(ol) - end
	if !reflect.DeepEqual(ol[end:], nl[len(nl)-tail:]) {
		t.Errorf("linhas depois de hooks mudaram:\n%q\n%q", ol[end:], nl[len(nl)-tail:])
	}
	if !bytes.HasSuffix(got, []byte("}\n")) {
		t.Error("saída deveria terminar com \\n")
	}
}

func indexOf(lines []string, s string) int {
	for i, l := range lines {
		if l == s {
			return i
		}
	}
	return -1
}

func TestInstallLinhasDeGrupoJaIndentadoFicamIguais(t *testing.T) {
	orig := []byte(`{
  "hooks": {
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "outro"
          }
        ]
      }
    ]
  }
}
`)
	settings, dir := setupSettings(t, orig)
	mustInstall(t, settings, dir)
	got := string(readFile(t, settings))
	want := `{
  "hooks": {
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "outro"
          }
        ]
      },
      {
        "hooks": [
          {
            "type": "command",
            "command": "` + filepath.Join(dir, "fleet-hook.sh") + `",
            "timeout": 5
          }
        ]
      }
    ],
`
	if !strings.HasPrefix(got, want) {
		t.Errorf("saída inesperada:\n%s", got)
	}
}

func TestInstallDuasVezesNadaAFazer(t *testing.T) {
	settings, dir := setupSettings(t, readFixture(t))
	mustInstall(t, settings, dir)
	first := readFile(t, settings)
	if n := len(backups(t, settings)); n != 1 {
		t.Fatalf("primeiro install: %d backups, queria 1", n)
	}

	out := mustInstall(t, settings, dir)
	if !strings.Contains(out, "nada a fazer") {
		t.Errorf("segundo install deveria dizer \"nada a fazer\":\n%s", out)
	}
	if strings.Contains(out, "Script do hook gravado") {
		t.Errorf("script não mudou e não deveria ser regravado:\n%s", out)
	}
	if !bytes.Equal(first, readFile(t, settings)) {
		t.Error("segundo install alterou o arquivo")
	}
	if n := len(backups(t, settings)); n != 1 {
		t.Errorf("segundo install criou backup: %d backups", n)
	}
}

func TestUninstallRemoveSoEntradasDoFleet(t *testing.T) {
	orig := readFixture(t)
	settings, dir := setupSettings(t, orig)
	mustInstall(t, settings, dir)
	out := mustUninstall(t, settings)
	got := readFile(t, settings)

	var a, b any
	if err := json.Unmarshal(orig, &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(got, &b); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Errorf("uninstall não devolveu o original:\n%s", got)
	}
	if bytes.Contains(got, []byte("fleet-hook.sh")) {
		t.Error("sobrou entrada do claude-fleet")
	}
	if !strings.Contains(out, "não foi apagado") || !strings.Contains(out, filepath.Join(dir, "fleet-hook.sh")) {
		t.Errorf("saída deveria avisar que o script ficou:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "fleet-hook.sh")); err != nil {
		t.Errorf("uninstall não deveria apagar o script: %v", err)
	}

	// Segundo uninstall: nada a fazer.
	if out := mustUninstall(t, settings); !strings.Contains(out, "nada a fazer") {
		t.Errorf("segundo uninstall: %s", out)
	}
}

func TestUninstallDeixaHooksVazio(t *testing.T) {
	settings, dir := setupSettings(t, []byte(`{"model": "x"}`))
	mustInstall(t, settings, dir)
	mustUninstall(t, settings)
	want := "{\n  \"model\": \"x\",\n  \"hooks\": {}\n}\n"
	if got := string(readFile(t, settings)); got != want {
		t.Errorf("queria\n%s\nveio\n%s", want, got)
	}
}

func TestUninstallNaoMexeEmGrupoMisto(t *testing.T) {
	orig := `{"hooks": {"Stop": [{"hooks": [{"type": "command", "command": "/x/fleet-hook.sh"}, {"type": "command", "command": "outro"}]}]}}`
	settings, _ := setupSettings(t, []byte(orig))
	out := mustUninstall(t, settings)
	if !strings.Contains(out, "nada a fazer") || string(readFile(t, settings)) != orig {
		t.Errorf("grupo misto não deveria ser removido:\n%s", out)
	}
}

func TestInstallTrocaCaminhoAntigo(t *testing.T) {
	orig := `{
  "hooks": {
    "Stop": [
      {"hooks": [{"type": "command", "command": "/antigo/fleet-hook.sh", "timeout": 5}]},
      {"hooks": [{"type": "command", "command": "alheio"}]},
      {"hooks": [{"type": "command", "command": "/outro-antigo/fleet-hook.sh"}]}
    ]
  }
}
`
	settings, dir := setupSettings(t, []byte(orig))
	mustInstall(t, settings, dir)
	got := readFile(t, settings)
	if bytes.Contains(got, []byte("antigo/fleet-hook.sh")) {
		t.Errorf("caminho antigo continua:\n%s", got)
	}
	stop := eventGroups(t, got)["Stop"]
	if len(stop) != 2 || compact(t, stop[0]) != wantGroup(dir) || compact(t, stop[1]) != `{"hooks":[{"type":"command","command":"alheio"}]}` {
		t.Errorf("Stop inesperado: %s", stop)
	}
}

func TestInstallSettingsAusente(t *testing.T) {
	settings, dir := setupSettings(t, nil)
	mustInstall(t, settings, dir)
	got := readFile(t, settings)
	if n := len(eventGroups(t, got)); n != len(fleetEvents) {
		t.Errorf("%d eventos, queria %d", n, len(fleetEvents))
	}
	if len(backups(t, settings)) != 0 {
		t.Error("não deveria haver backup de arquivo que não existia")
	}
	if st, _ := os.Stat(settings); st.Mode().Perm() != 0o600 {
		t.Errorf("permissão %v, queria 0600", st.Mode().Perm())
	}

	// uninstall de settings ausente: nada a fazer e nada criado.
	other := filepath.Join(t.TempDir(), "settings.json")
	if out := mustUninstall(t, other); !strings.Contains(out, "nada a fazer") {
		t.Errorf("saída: %s", out)
	}
	if _, err := os.Stat(other); !os.IsNotExist(err) {
		t.Error("uninstall criou o arquivo")
	}
}

func TestJSONInvalidoNaoEscreve(t *testing.T) {
	for _, content := range []string{`{"hooks": {`, `{"a": 1} lixo`, `[1, 2]`, `{"hooks": []}`, `{"hooks": {"Stop": {}}}`} {
		settings, dir := setupSettings(t, []byte(content))
		var out bytes.Buffer
		if err := installHooks(opts(settings, dir, "", true, &out)); err == nil {
			t.Errorf("install de %q deveria falhar", content)
		}
		if content != `{"hooks": {"Stop": {}}}` {
			if err := uninstallHooks(opts(settings, dir, "", true, &out)); err == nil {
				t.Errorf("uninstall de %q deveria falhar", content)
			}
		}
		if string(readFile(t, settings)) != content {
			t.Errorf("%q: arquivo alterado", content)
		}
		if len(backups(t, settings)) != 0 {
			t.Errorf("%q: backup criado", content)
		}
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Errorf("%q: script gravado apesar do erro", content)
		}
	}
}

func TestBackupComConteudoOriginal(t *testing.T) {
	orig := readFixture(t)
	settings, dir := setupSettings(t, orig)
	out := mustInstall(t, settings, dir)
	baks := backups(t, settings)
	if len(baks) != 1 {
		t.Fatalf("%d backups, queria 1", len(baks))
	}
	if want := settings + ".bak-20261015-143005"; baks[0] != want {
		t.Errorf("backup %s, queria %s", baks[0], want)
	}
	if !bytes.Equal(readFile(t, baks[0]), orig) {
		t.Error("backup não tem o conteúdo original")
	}
	if !strings.Contains(out, baks[0]) {
		t.Errorf("saída deveria citar o backup:\n%s", out)
	}
	if st, _ := os.Stat(settings); st.Mode().Perm() != 0o640 {
		t.Errorf("permissão do settings virou %v, queria 0640", st.Mode().Perm())
	}

	// Uninstall no mesmo segundo não sobrescreve o backup anterior.
	mustUninstall(t, settings)
	if baks := backups(t, settings); len(baks) != 2 {
		t.Errorf("queria 2 backups, veio %v", baks)
	}
	if !bytes.Equal(readFile(t, settings+".bak-20261015-143005"), orig) {
		t.Error("primeiro backup foi sobrescrito")
	}
}

func TestConfirmacao(t *testing.T) {
	orig := readFixture(t)
	for _, tc := range []struct {
		stdin string
		apply bool
	}{{"n\n", false}, {"\n", false}, {"", false}, {"s\n", true}, {"Sim\n", true}} {
		settings, dir := setupSettings(t, orig)
		var out bytes.Buffer
		if err := installHooks(opts(settings, dir, tc.stdin, false, &out)); err != nil {
			t.Fatal(err)
		}
		s := out.String()
		if !strings.Contains(s, "[s/N]") || !strings.Contains(s, "\n+ ") || !strings.Contains(s, "fleet-hook.sh") {
			t.Errorf("%q: faltou diff ou pergunta:\n%s", tc.stdin, s)
		}
		changed := !bytes.Equal(readFile(t, settings), orig)
		if changed != tc.apply {
			t.Errorf("%q: alterou=%t, queria %t", tc.stdin, changed, tc.apply)
		}
		if n := len(backups(t, settings)); (n == 1) != tc.apply {
			t.Errorf("%q: %d backups", tc.stdin, n)
		}
		if _, err := os.Stat(dir); os.IsNotExist(err) == tc.apply {
			t.Errorf("%q: script gravado=%t, queria %t", tc.stdin, !os.IsNotExist(err), tc.apply)
		}
	}
}

func TestDiffMostraLinhas(t *testing.T) {
	var out bytes.Buffer
	before := "a\nb\nc\nd\ne\nf\ng\nh\ni\n"
	after := "a\nb\nc\nd\nX\ne\nf\ng\nh\ni\n"
	writeDiff(&out, before, after)
	want := "@@\n  b\n  c\n  d\n+ X\n  e\n  f\n  g\n"
	if out.String() != want {
		t.Errorf("diff:\n%s", out.String())
	}
	out.Reset()
	writeDiff(&out, "x\n", "y\n")
	if out.String() != "- x\n+ y\n" {
		t.Errorf("diff:\n%s", out.String())
	}
}

func TestScriptGravadoComEmbed(t *testing.T) {
	settings, dir := setupSettings(t, readFixture(t))
	mustInstall(t, settings, dir)
	script := filepath.Join(dir, "fleet-hook.sh")
	st, err := os.Stat(script)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o755 {
		t.Errorf("modo %v, queria 0755", st.Mode().Perm())
	}
	if string(readFile(t, script)) != hook.Script || hook.Script == "" {
		t.Error("conteúdo do script difere do embed")
	}

	// Script alterado à mão volta ao embed mesmo sem mudança no settings.
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := mustInstall(t, settings, dir)
	if !strings.Contains(out, "Script do hook gravado") || !strings.Contains(out, "nada a fazer") {
		t.Errorf("saída: %s", out)
	}
	if st, _ := os.Stat(script); string(readFile(t, script)) != hook.Script || st.Mode().Perm() != 0o755 {
		t.Error("script não foi restaurado")
	}
}

func TestInstallSegueSymlink(t *testing.T) {
	orig := readFixture(t)
	real, dir := setupSettings(t, orig)
	link := filepath.Join(t.TempDir(), "settings.json")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	mustInstall(t, link, dir)
	if st, err := os.Lstat(link); err != nil || st.Mode()&os.ModeSymlink == 0 {
		t.Fatal("o symlink foi substituído por arquivo")
	}
	if bytes.Equal(readFile(t, real), orig) {
		t.Error("o alvo do symlink não foi atualizado")
	}
}

func TestParseHooksFlags(t *testing.T) {
	e := env(map[string]string{"HOME": "/home/x"})
	o, err := parseHooksFlags("install-hooks", nil, e, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if o.settings != "/home/x/.claude/settings.json" || o.dir != "/home/x/.claude/claude-fleet" || o.yes {
		t.Errorf("padrões: %+v", o)
	}
	o, err = parseHooksFlags("install-hooks", []string{"-settings", "/t/s.json", "-dir", "/t/d", "-yes"}, env(nil), io.Discard)
	if err != nil || o.settings != "/t/s.json" || o.dir != "/t/d" || !o.yes {
		t.Errorf("flags: %+v, %v", o, err)
	}
	if _, err := parseHooksFlags("uninstall-hooks", []string{"-dir", "/t/d"}, e, io.Discard); err == nil {
		t.Error("uninstall-hooks não aceita -dir")
	}
	if _, err := parseHooksFlags("install-hooks", []string{"extra"}, e, io.Discard); err == nil {
		t.Error("argumento posicional deveria falhar")
	}
	if code := runHooksCmd("install-hooks", []string{"-x"}, e, nil, io.Discard, io.Discard); code != 2 {
		t.Errorf("flag inválida: código %d, queria 2", code)
	}
}

func TestInstallHooksUsaClaudeConfigDir(t *testing.T) {
	home, conta := t.TempDir(), t.TempDir()
	e := env(map[string]string{"HOME": home, "CLAUDE_CONFIG_DIR": conta})

	var out bytes.Buffer
	if code := runHooksCmd("install-hooks", []string{"-yes"}, e, nil, &out, io.Discard); code != 0 {
		t.Fatalf("install-hooks: código %d\n%s", code, out.String())
	}
	data, err := os.ReadFile(filepath.Join(conta, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(conta, "claude-fleet", hookScriptName)
	if !strings.Contains(string(data), script) {
		t.Errorf("settings da conta sem o comando %s:\n%s", script, data)
	}
	if _, err := os.Stat(script); err != nil {
		t.Errorf("script não gravado na conta: %v", err)
	}
	if !strings.Contains(out.String(), filepath.Join(conta, "settings.json")) {
		t.Errorf("saída não diz qual settings altera:\n%s", out.String())
	}
	if ents, _ := os.ReadDir(home); len(ents) != 0 {
		t.Errorf("HOME foi tocado: %v", ents)
	}

	if code := runHooksCmd("uninstall-hooks", []string{"-yes"}, e, nil, io.Discard, io.Discard); code != 0 {
		t.Fatalf("uninstall-hooks: código %d", code)
	}
	data, _ = os.ReadFile(filepath.Join(conta, "settings.json"))
	if strings.Contains(string(data), hookScriptName) {
		t.Errorf("uninstall-hooks não removeu da conta:\n%s", data)
	}

	tests := []struct {
		name                  string
		vars                  map[string]string
		args                  []string
		wantSettings, wantDir string
	}{
		{"variável define os padrões", map[string]string{"HOME": "/home/x", "CLAUDE_CONFIG_DIR": "/home/x/.claude-p/"}, nil,
			"/home/x/.claude-p/settings.json", "/home/x/.claude-p/claude-fleet"},
		{"flags vencem a variável", map[string]string{"HOME": "/home/x", "CLAUDE_CONFIG_DIR": "/home/x/.claude-p"},
			[]string{"-settings", "/t/s.json", "-dir", "/t/d"}, "/t/s.json", "/t/d"},
		{"variável igual a ~/.claude é o padrão de sempre", map[string]string{"HOME": "/home/x", "CLAUDE_CONFIG_DIR": "/home/x/.claude"}, nil,
			"/home/x/.claude/settings.json", "/home/x/.claude/claude-fleet"},
		{"variável vazia cai no HOME", map[string]string{"HOME": "/home/x", "CLAUDE_CONFIG_DIR": ""}, nil,
			"/home/x/.claude/settings.json", "/home/x/.claude/claude-fleet"},
		{"variável dispensa o HOME", map[string]string{"CLAUDE_CONFIG_DIR": "/c"}, nil, "/c/settings.json", "/c/claude-fleet"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o, err := parseHooksFlags("install-hooks", tt.args, env(tt.vars), io.Discard)
			if err != nil {
				t.Fatal(err)
			}
			if o.settings != tt.wantSettings || o.dir != tt.wantDir {
				t.Errorf("settings=%q dir=%q, queria %q e %q", o.settings, o.dir, tt.wantSettings, tt.wantDir)
			}
		})
	}
}

func TestInstallHooksRecusaConfigDirRelativo(t *testing.T) {
	for _, dir := range []string{"~/.claude-pessoal", "~", ".claude-pessoal", "./x"} {
		t.Run(dir, func(t *testing.T) {
			cwd := t.TempDir()
			t.Chdir(cwd)
			e := env(map[string]string{"HOME": t.TempDir(), "CLAUDE_CONFIG_DIR": dir})
			for _, cmd := range []string{"install-hooks", "uninstall-hooks"} {
				var errOut bytes.Buffer
				if code := runHooksCmd(cmd, []string{"-yes"}, e, nil, io.Discard, &errOut); code != 2 {
					t.Errorf("%s: código %d, queria 2", cmd, code)
				}
				if !strings.Contains(errOut.String(), "~") {
					t.Errorf("%s: mensagem não cita o ~: %q", cmd, errOut.String())
				}
			}
			if ents, _ := os.ReadDir(cwd); len(ents) != 0 {
				t.Errorf("criou algo no diretório atual: %v", ents)
			}
		})
	}
}
