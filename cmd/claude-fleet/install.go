package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/devpolatto/claude-fleet/hook"
)

const (
	hookScriptName       = "fleet-hook.sh"
	statusLineScriptName = "fleet-statusline.sh"
	hookTimeout          = 5 // segundos; o script já desiste em 1 s (AD-006)
	diffContext          = 3
)

// fleetEvents são os eventos que o Store consome, na ordem em que entram
// num settings que ainda não os tem.
var fleetEvents = []string{
	"SessionStart", "SessionEnd", "UserPromptSubmit",
	"PreToolUse", "PostToolUse", "PostToolUseFailure", "PostToolBatch",
	"PermissionRequest", "PermissionDenied", "Notification",
	"Stop", "StopFailure", "SubagentStart", "SubagentStop",
	"PreCompact", "PostCompact",
}

// hookOpts reúne flags e E/S dos subcomandos; stdin/stdout são injetáveis
// para os testes.
type hookOpts struct {
	settings string
	dir      string
	yes      bool
	in       io.Reader
	out      io.Writer
	now      func() time.Time
}

// runHooksCmd executa install-hooks ou uninstall-hooks e devolve o código de saída.
func runHooksCmd(name string, args []string, getenv func(string) string, in io.Reader, out, errOut io.Writer) int {
	o, err := parseHooksFlags(name, args, getenv, errOut)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintf(errOut, "claude-fleet %s: %v\n", name, err)
		return 2
	}
	o.in, o.out, o.now = in, out, time.Now
	if name == "install-hooks" {
		err = installHooks(o)
	} else {
		err = uninstallHooks(o)
	}
	if err != nil {
		fmt.Fprintf(errOut, "claude-fleet %s: %v\n", name, err)
		return 1
	}
	return 0
}

func parseHooksFlags(name string, args []string, getenv func(string) string, errOut io.Writer) (hookOpts, error) {
	fs := flag.NewFlagSet("claude-fleet "+name, flag.ContinueOnError)
	fs.SetOutput(errOut)
	var o hookOpts
	fs.StringVar(&o.settings, "settings", "", "settings do Claude Code (padrão: $CLAUDE_CONFIG_DIR/settings.json ou ~/.claude/settings.json)")
	if name == "install-hooks" {
		fs.StringVar(&o.dir, "dir", "", "diretório onde o script do hook é gravado (padrão: $CLAUDE_CONFIG_DIR/claude-fleet ou ~/.claude/claude-fleet)")
	}
	fs.BoolVar(&o.yes, "yes", false, "aplica sem pedir confirmação")
	if err := fs.Parse(args); err != nil {
		return hookOpts{}, err
	}
	if fs.NArg() > 0 {
		return hookOpts{}, fmt.Errorf("argumento inesperado: %q", fs.Arg(0))
	}
	if o.settings == "" || (name == "install-hooks" && o.dir == "") {
		base, err := configDir(getenv)
		if err != nil {
			return hookOpts{}, err
		}
		if o.settings == "" {
			o.settings = filepath.Join(base, "settings.json")
		}
		if name == "install-hooks" && o.dir == "" {
			o.dir = filepath.Join(base, "claude-fleet")
		}
	}
	return o, nil
}

// configDir é o diretório de configuração da conta do Claude Code:
// $CLAUDE_CONFIG_DIR quando definido, senão ~/.claude.
func configDir(getenv func(string) string) (string, error) {
	if dir := getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		// Um ~ que o shell não expandiu (por exemplo, entre aspas) viraria
		// um diretório relativo chamado "~" no diretório atual.
		if !filepath.IsAbs(dir) {
			return "", fmt.Errorf("CLAUDE_CONFIG_DIR precisa ser absoluto, veio %q (o shell não expandiu o ~?)", dir)
		}
		return filepath.Clean(dir), nil
	}
	home := getenv("HOME")
	if home == "" {
		return "", errors.New("HOME não definido; informe -settings e -dir")
	}
	return filepath.Join(home, ".claude"), nil
}

// installHooks grava os scripts, registra o hook nos eventos do settings e
// o statusLine do uso do plano.
func installHooks(o hookOpts) error {
	dir, err := filepath.Abs(o.dir)
	if err != nil {
		return err
	}
	script := filepath.Join(dir, hookScriptName)
	statusScript := filepath.Join(dir, statusLineScriptName)

	s, err := readSettings(o.settings)
	if err != nil {
		return err
	}
	updated, res, err := applyInstall(s.data, shellQuote(script), shellQuote(statusScript))
	if err != nil {
		return fmt.Errorf("%s: %w", s.path, err)
	}
	if res.statusLine == statusLineForeign {
		fmt.Fprintf(o.out, "%s já tem um statusLine (%s); o uso do plano fica sem fonte. Veja features/uso-plano.\n", s.path, res.foreign)
	}

	if !res.changed() {
		if err := syncScripts(o.out, script, statusScript); err != nil {
			return err
		}
		what := "o hook do claude-fleet em todos os eventos e o statusLine do claude-fleet"
		if res.statusLine == statusLineForeign {
			what = "o hook do claude-fleet em todos os eventos"
		}
		fmt.Fprintf(o.out, "%s já tem %s; nada a fazer.\n", s.path, what)
		return nil
	}

	if !confirm(o, s, updated) {
		fmt.Fprintln(o.out, "Cancelado; nada foi gravado.")
		return nil
	}
	if err := syncScripts(o.out, script, statusScript); err != nil {
		return err
	}
	if err := commitSettings(o, s, updated); err != nil {
		return err
	}
	if res.hooks {
		fmt.Fprintf(o.out, "Hook registrado em %d eventos (comando %s).\n", len(fleetEvents), script)
	}
	if res.statusLine == statusLineAdded || res.statusLine == statusLineReplaced {
		fmt.Fprintf(o.out, "statusLine do claude-fleet registrado (comando %s); ele não imprime nada, e o Claude Code esconde as dicas do rodapé enquanto houver um statusLine.\n", statusScript)
	}
	return nil
}

// syncScripts grava o script do hook e o do status line.
func syncScripts(out io.Writer, hookPath, statusPath string) error {
	if err := syncScript(out, hookPath, hook.Script, "do hook"); err != nil {
		return err
	}
	return syncScript(out, statusPath, hook.StatusLine, "do status line")
}

// uninstallHooks remove do settings só os grupos e o statusLine do claude-fleet.
func uninstallHooks(o hookOpts) error {
	s, err := readSettings(o.settings)
	if err != nil {
		return err
	}
	updated, res, err := applyUninstall(s.data)
	if err != nil {
		return fmt.Errorf("%s: %w", s.path, err)
	}
	if !res.hooks && !res.statusLine {
		fmt.Fprintf(o.out, "%s não tem hooks nem statusLine do claude-fleet; nada a fazer.\n", s.path)
		return nil
	}
	if !confirm(o, s, updated) {
		fmt.Fprintln(o.out, "Cancelado; nada foi gravado.")
		return nil
	}
	if err := commitSettings(o, s, updated); err != nil {
		return err
	}
	if res.hooks {
		fmt.Fprintf(o.out, "Hook do claude-fleet removido de %s.\n", s.path)
	}
	if res.statusLine {
		fmt.Fprintf(o.out, "statusLine do claude-fleet removido de %s.\n", s.path)
	}
	for _, c := range res.removed {
		fmt.Fprintf(o.out, "O script %s não foi apagado; remova-o à mão se não for mais usar.\n", c)
	}
	return nil
}

// settingsFile é o settings lido do disco. path já tem os symlinks resolvidos,
// para que a escrita atômica troque o arquivo de verdade e não o link.
type settingsFile struct {
	path   string
	data   []byte // nil se o arquivo não existe
	perm   fs.FileMode
	exists bool
}

func readSettings(path string) (settingsFile, error) {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return settingsFile{path: path, perm: 0o600}, nil
	}
	if err != nil {
		return settingsFile{}, err
	}
	st, err := os.Stat(path)
	if err != nil {
		return settingsFile{}, err
	}
	return settingsFile{path: path, data: data, perm: st.Mode().Perm(), exists: true}, nil
}

// syncScript grava content com modo 0755, só se conteúdo ou modo mudaram.
// label completa as mensagens ("do hook", "do status line").
func syncScript(out io.Writer, path, content, label string) error {
	cur, err := os.ReadFile(path)
	if err == nil && string(cur) == content {
		if st, err := os.Stat(path); err == nil && st.Mode().Perm() == 0o755 {
			return nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := writeAtomic(path, []byte(content), 0o755); err != nil {
		return fmt.Errorf("gravar o script %s: %w", label, err)
	}
	fmt.Fprintf(out, "Script %s gravado em %s.\n", label, path)
	return nil
}

// confirm mostra o diff e, sem -yes, pede confirmação no stdin.
func confirm(o hookOpts, s settingsFile, updated []byte) bool {
	fmt.Fprintf(o.out, "Mudanças em %s:\n", s.path)
	writeDiff(o.out, string(s.data), string(updated))
	if o.yes {
		return true
	}
	fmt.Fprintf(o.out, "Aplicar? [s/N] ")
	line, _ := bufio.NewReader(o.in).ReadString('\n')
	fmt.Fprintln(o.out)
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "s", "sim", "y", "yes":
		return true
	}
	return false
}

// commitSettings guarda o backup datado do original e grava o novo de forma atômica.
func commitSettings(o hookOpts, s settingsFile, updated []byte) error {
	if s.exists {
		bak, err := writeBackup(s, o.now())
		if err != nil {
			return fmt.Errorf("backup: %w", err)
		}
		fmt.Fprintf(o.out, "Backup do original em %s.\n", bak)
	} else if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	if err := writeAtomic(s.path, updated, s.perm); err != nil {
		return fmt.Errorf("gravar %s: %w", s.path, err)
	}
	fmt.Fprintf(o.out, "%s gravado.\n", s.path)
	return nil
}

// writeBackup cria <settings>.bak-AAAAMMDD-HHMMSS sem sobrescrever um backup
// existente (no mesmo segundo, acrescenta -1, -2, ...).
func writeBackup(s settingsFile, now time.Time) (string, error) {
	base := s.path + ".bak-" + now.Format("20060102-150405")
	for i := 0; ; i++ {
		name := base
		if i > 0 {
			name = fmt.Sprintf("%s-%d", base, i)
		}
		f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, s.perm)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if _, err := f.Write(s.data); err != nil {
			f.Close()
			return "", err
		}
		return name, f.Close()
	}
}

// writeAtomic grava num temporário do mesmo diretório e renomeia por cima.
func writeAtomic(path string, data []byte, perm fs.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	ok := false
	defer func() {
		if !ok {
			f.Close()
			os.Remove(tmp)
		}
	}()
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Chmod(perm); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	ok = true
	return nil
}

// --- Edição do JSON preservando o formato ---
//
// O objeto de topo e o objeto "hooks" são lidos como listas ordenadas de
// (chave, json.RawMessage). Valores que não mudam voltam com os bytes
// originais; só os arrays de eventos alterados são remontados, e mesmo neles
// os grupos alheios entram byte a byte. A saída usa indentação de 2 espaços.

type member struct {
	key string
	raw json.RawMessage
}

// parseObject lê um objeto JSON mantendo a ordem das chaves e os bytes dos valores.
// Conteúdo vazio vale como {}.
func parseObject(data []byte) ([]member, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, nil
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return nil, jsonErr(err)
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, errors.New("JSON inválido: era esperado um objeto")
	}
	var ms []member
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, jsonErr(err)
		}
		key, _ := tok.(string)
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, jsonErr(err)
		}
		ms = append(ms, member{key, raw})
	}
	if _, err := dec.Token(); err != nil {
		return nil, jsonErr(err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("JSON inválido: conteúdo depois do fim do objeto")
	}
	return ms, nil
}

func jsonErr(err error) error {
	var se *json.SyntaxError
	if errors.As(err, &se) {
		return fmt.Errorf("JSON inválido no byte %d: %v", se.Offset, err)
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return errors.New("JSON inválido: fim inesperado do arquivo")
	}
	return fmt.Errorf("JSON inválido: %w", err)
}

// hooksSection separa o objeto de topo e o objeto "hooks" (idx < 0 se ausente).
func hooksSection(data []byte) (top []member, idx int, events []member, err error) {
	top, err = parseObject(data)
	if err != nil {
		return nil, 0, nil, err
	}
	idx = -1
	for i, m := range top {
		if m.key == "hooks" {
			idx = i
		}
	}
	if idx >= 0 {
		if t := bytes.TrimSpace(top[idx].raw); len(t) == 0 || t[0] != '{' {
			return nil, 0, nil, errors.New(`"hooks" não é um objeto`)
		}
		if events, err = parseObject(top[idx].raw); err != nil {
			return nil, 0, nil, err
		}
	}
	return top, idx, events, nil
}

// fleetGroup é o grupo que o instalador acrescenta: sem matcher e sem async.
type fleetGroup struct {
	Hooks []fleetHook `json:"hooks"`
}

type fleetHook struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Timeout int    `json:"timeout"`
}

// isFleetGroup diz se o grupo é do claude-fleet: todo comando dele termina em
// /fleet-hook.sh. Um grupo misto (com comandos alheios) não é tocado.
func isFleetGroup(raw json.RawMessage) bool {
	var g struct {
		Hooks []struct {
			Command string `json:"command"`
		} `json:"hooks"`
	}
	if json.Unmarshal(raw, &g) != nil || len(g.Hooks) == 0 {
		return false
	}
	for _, h := range g.Hooks {
		if !isScriptCommand(h.Command, hookScriptName) {
			return false
		}
	}
	return true
}

func sameJSON(a, b []byte) bool {
	var x, y any
	return json.Unmarshal(a, &x) == nil && json.Unmarshal(b, &y) == nil && reflect.DeepEqual(x, y)
}

// statusLineAction diz o que o instalador fez com o statusLine.
type statusLineAction int

const (
	statusLineKept     statusLineAction = iota // já era o do claude-fleet
	statusLineAdded                            // não havia statusLine
	statusLineReplaced                         // era um statusLine antigo do claude-fleet
	statusLineForeign                          // alheio: não tocado
)

// installResult resume o que applyInstall mudou.
type installResult struct {
	hooks      bool // algum evento ganhou ou trocou a entrada do claude-fleet
	statusLine statusLineAction
	foreign    string // comando (ou JSON) do statusLine alheio
}

func (r installResult) changed() bool {
	return r.hooks || r.statusLine == statusLineAdded || r.statusLine == statusLineReplaced
}

// statusLineValue é o statusLine que o instalador grava: sem padding e sem
// refreshInterval.
type statusLineValue struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

// statusLineCommand devolve o command de um statusLine que é objeto com
// command string; ok é false para qualquer outra forma.
func statusLineCommand(raw json.RawMessage) (cmd string, ok bool) {
	var v map[string]json.RawMessage
	if json.Unmarshal(raw, &v) != nil || v == nil {
		return "", false
	}
	if json.Unmarshal(v["command"], &cmd) != nil {
		return "", false
	}
	return cmd, true
}

// isFleetStatusLine diz se o statusLine é do claude-fleet: objeto com command
// terminando em /fleet-statusline.sh.
func isFleetStatusLine(raw json.RawMessage) bool {
	cmd, ok := statusLineCommand(raw)
	return ok && isScriptCommand(cmd, statusLineScriptName)
}

// shellQuote devolve o caminho pronto para o comando do settings, que o
// Claude Code roda num shell. Caminho só com caracteres seguros vai como está
// (o mesmo que versões anteriores gravaram); o resto vai entre aspas simples,
// para espaço, $ ou crase no diretório não quebrarem nem executarem nada.
func shellQuote(path string) string {
	safe := path != ""
	for _, r := range path {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("/._-+@%:,=", r)) {
			safe = false
			break
		}
	}
	if safe {
		return path
	}
	return "'" + strings.ReplaceAll(path, "'", `'\''`) + "'"
}

// isScriptCommand diz se cmd chama o script name do claude-fleet, com o
// caminho entre aspas (shellQuote) ou não.
func isScriptCommand(cmd, name string) bool {
	return strings.HasSuffix(cmd, "/"+name) || strings.HasSuffix(cmd, "/"+name+"'")
}

// applyStatusLine registra o statusLine do claude-fleet no objeto de topo,
// sem tocar num statusLine alheio.
func applyStatusLine(top []member, command string) ([]member, installResult, error) {
	want, err := marshalIndent(statusLineValue{Type: "command", Command: command}, "  ")
	if err != nil {
		return nil, installResult{}, err
	}
	for i, m := range top {
		if m.key != "statusLine" {
			continue
		}
		switch {
		case !isFleetStatusLine(m.raw):
			foreign, ok := statusLineCommand(m.raw)
			if !ok {
				var b bytes.Buffer
				if json.Compact(&b, m.raw) == nil {
					foreign = b.String()
				} else {
					foreign = string(m.raw)
				}
			}
			return top, installResult{statusLine: statusLineForeign, foreign: foreign}, nil
		case sameJSON(m.raw, want):
			return top, installResult{statusLine: statusLineKept}, nil
		default:
			top[i].raw = want
			return top, installResult{statusLine: statusLineReplaced}, nil
		}
	}
	return append(top, member{"statusLine", want}), installResult{statusLine: statusLineAdded}, nil
}

// applyInstall devolve o settings com o hook em todos os fleetEvents e o
// statusLine do claude-fleet (salvo um statusLine alheio). Quando nada
// precisou mudar, out é o próprio data.
func applyInstall(data []byte, command, statusCommand string) (out []byte, res installResult, err error) {
	top, idx, events, err := hooksSection(data)
	if err != nil {
		return nil, installResult{}, err
	}
	want, err := marshalIndent(fleetGroup{Hooks: []fleetHook{{Type: "command", Command: command, Timeout: hookTimeout}}}, "      ")
	if err != nil {
		return nil, installResult{}, err
	}
	changed := false

	pos := make(map[string]int, len(events))
	for i, m := range events {
		pos[m.key] = i
	}
	for _, ev := range fleetEvents {
		i, ok := pos[ev]
		if !ok {
			events = append(events, member{ev, renderArray([]json.RawMessage{want}, "    ")})
			changed = true
			continue
		}
		var groups []json.RawMessage
		if err := json.Unmarshal(events[i].raw, &groups); err != nil {
			return nil, installResult{}, fmt.Errorf(`"hooks.%s" não é uma lista de grupos`, ev)
		}
		var mine []int
		for j, g := range groups {
			if isFleetGroup(g) {
				mine = append(mine, j)
			}
		}
		if len(mine) == 1 && sameJSON(groups[mine[0]], want) {
			continue
		}
		// Troca a entrada antiga no lugar da primeira (ou acrescenta no fim)
		// e descarta duplicatas; grupos alheios ficam na ordem original.
		next := make([]json.RawMessage, 0, len(groups)+1)
		placed := false
		for j, g := range groups {
			if len(mine) > 0 && j == mine[0] {
				next = append(next, want)
				placed = true
				continue
			}
			if isFleetGroup(g) {
				continue
			}
			next = append(next, g)
		}
		if !placed {
			next = append(next, want)
		}
		events[i].raw = renderArray(next, "    ")
		changed = true
	}
	if changed {
		hooksRaw := renderObject(events, "  ")
		if idx >= 0 {
			top[idx].raw = hooksRaw
		} else {
			top = append(top, member{"hooks", hooksRaw})
		}
	}
	top, res, err = applyStatusLine(top, statusCommand)
	if err != nil {
		return nil, installResult{}, err
	}
	res.hooks = changed
	if !res.changed() {
		return data, res, nil
	}
	return append(renderObject(top, ""), '\n'), res, nil
}

// uninstallResult resume o que applyUninstall removeu.
type uninstallResult struct {
	hooks      bool     // algum grupo do claude-fleet saiu de hooks
	statusLine bool     // o statusLine do claude-fleet saiu
	removed    []string // comandos removidos, sem repetição
}

// applyUninstall remove os grupos do claude-fleet de qualquer evento e o
// statusLine, se for do claude-fleet. Quando nada sai, out é o próprio data.
func applyUninstall(data []byte) (out []byte, res uninstallResult, err error) {
	top, idx, events, err := hooksSection(data)
	if err != nil {
		return nil, uninstallResult{}, err
	}
	var removed []string
	seen := map[string]bool{}
	kept := events[:0:0]
	for _, m := range events {
		var groups []json.RawMessage
		if json.Unmarshal(m.raw, &groups) != nil {
			kept = append(kept, m) // valor que não é lista: não é nosso
			continue
		}
		var next []json.RawMessage
		for _, g := range groups {
			if !isFleetGroup(g) {
				next = append(next, g)
				continue
			}
			var fg fleetGroup
			_ = json.Unmarshal(g, &fg)
			for _, h := range fg.Hooks {
				if !seen[h.Command] {
					seen[h.Command] = true
					removed = append(removed, h.Command)
				}
			}
		}
		switch {
		case len(next) == len(groups):
			kept = append(kept, m)
		case len(next) > 0:
			kept = append(kept, member{m.key, renderArray(next, "    ")})
		}
	}
	res.hooks = len(removed) > 0
	if res.hooks {
		top[idx].raw = renderObject(kept, "  ")
	}
	for i, m := range top {
		if m.key == "statusLine" && isFleetStatusLine(m.raw) {
			cmd, _ := statusLineCommand(m.raw)
			if !seen[cmd] {
				removed = append(removed, cmd)
			}
			top = append(top[:i:i], top[i+1:]...)
			res.statusLine = true
			break
		}
	}
	res.removed = removed
	if !res.hooks && !res.statusLine {
		return data, res, nil
	}
	return append(renderObject(top, ""), '\n'), res, nil
}

// renderObject monta um objeto com as chaves uma por linha; ind é a
// indentação da linha onde o objeto começa.
func renderObject(ms []member, ind string) json.RawMessage {
	if len(ms) == 0 {
		return json.RawMessage("{}")
	}
	var b bytes.Buffer
	b.WriteString("{\n")
	for i, m := range ms {
		key, _ := marshalIndent(m.key, "")
		b.WriteString(ind + "  ")
		b.Write(key)
		b.WriteString(": ")
		b.Write(m.raw)
		if i < len(ms)-1 {
			b.WriteByte(',')
		}
		b.WriteByte('\n')
	}
	b.WriteString(ind + "}")
	return b.Bytes()
}

// renderArray monta um array com um elemento por linha, cada um com seus bytes.
func renderArray(elems []json.RawMessage, ind string) json.RawMessage {
	var b bytes.Buffer
	b.WriteString("[\n")
	for i, e := range elems {
		b.WriteString(ind + "  ")
		b.Write(e)
		if i < len(elems)-1 {
			b.WriteByte(',')
		}
		b.WriteByte('\n')
	}
	b.WriteString(ind + "]")
	return b.Bytes()
}

// marshalIndent codifica v sem escapar <, > e & e com prefixo nas linhas seguintes.
func marshalIndent(v any, prefix string) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent(prefix, "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(b.Bytes(), "\n"), nil
}

// --- Diff de linhas ---

type diffLine struct {
	op   byte // ' ', '-' ou '+'
	text string
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

// lineDiff calcula um diff de linhas por LCS.
func lineDiff(a, b []string) []diffLine {
	n, m := len(a), len(b)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	var out []diffLine
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			out = append(out, diffLine{' ', a[i]})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			out = append(out, diffLine{'-', a[i]})
			i++
		default:
			out = append(out, diffLine{'+', b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		out = append(out, diffLine{'-', a[i]})
	}
	for ; j < m; j++ {
		out = append(out, diffLine{'+', b[j]})
	}
	return out
}

// writeDiff imprime só as linhas alteradas com diffContext linhas de contexto;
// trechos omitidos aparecem como "@@".
func writeDiff(w io.Writer, before, after string) {
	d := lineDiff(splitLines(before), splitLines(after))
	show := make([]bool, len(d))
	for i, l := range d {
		if l.op == ' ' {
			continue
		}
		for k := max(0, i-diffContext); k <= min(len(d)-1, i+diffContext); k++ {
			show[k] = true
		}
	}
	gap := false
	for i, l := range d {
		if !show[i] {
			gap = true
			continue
		}
		if gap {
			fmt.Fprintln(w, "@@")
			gap = false
		}
		fmt.Fprintf(w, "%c %s\n", l.op, l.text)
	}
}
