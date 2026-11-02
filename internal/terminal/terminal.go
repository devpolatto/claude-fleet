// Package terminal liga uma sessão do Claude Code ao pane do tmux onde ela
// roda e abre uma janela nova do Ptyxis presa a esse pane (AD-010).
//
// A ligação vem de <CLAUDE_CONFIG_DIR>/sessions/<pid>.json, que o Claude Code
// grava com sessionId, procStart e, dentro do tmux, o campo tmux
// ("<sessão>:@<janela>.%<pane>"). Só esses campos são lidos. O pacote só
// executa dois comandos fixos (ver allowed): o display-message do tmux e o
// systemd-run que abre o Ptyxis.
package terminal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Estado diz se a sessão roda no tmux.
type Estado string

const (
	// EstadoTmux: o pane foi achado e conferido.
	EstadoTmux Estado = "tmux"
	// EstadoFora: a sessão está viva, mas não roda dentro do tmux.
	EstadoFora Estado = "fora"
	// EstadoDesconhecido: sem arquivo da sessão, processo morto, pane que
	// não bate ou tmux fora do ar.
	EstadoDesconhecido Estado = "desconhecido"
)

// Location é onde a sessão roda. Pane é o ID do tmux (%N), de uso interno.
type Location struct {
	Estado Estado
	Rotulo string // "tmux <sessão>:<janela>.<pane>"; vazio fora de EstadoTmux
	Pane   string
}

// Prazos dos comandos e limite de leitura dos arquivos de sessão.
const (
	locateTimeout   = 2 * time.Second
	openTimeout     = 5 * time.Second
	maxSessionBytes = 64 << 10
	maxRotuloRunes  = 64
)

var (
	sessionFileRe = regexp.MustCompile(`^([0-9]{1,10})\.json$`)
	tmuxFieldRe   = regexp.MustCompile(`^[0-9]+:@[0-9]+\.(%[0-9]{1,9})$`)
)

// Terminal localiza sessões e abre janelas.
type Terminal struct {
	run  Runner
	proc string // raiz do /proc
}

// New devolve o Terminal real (tmux, systemd-run e /proc da máquina).
func New() *Terminal { return &Terminal{run: execRunner{}, proc: "/proc"} }

// sessionFile são os campos lidos de sessions/<pid>.json; o resto do JSON é
// ignorado.
type sessionFile struct {
	PID       int             `json:"pid"`
	SessionID string          `json:"sessionId"`
	ProcStart json.RawMessage `json:"procStart"`
	Tmux      string          `json:"tmux"`
}

// Locate diz onde roda a sessão sessionID da conta em configDir.
func (t *Terminal) Locate(ctx context.Context, configDir, sessionID string) Location {
	unknown := Location{Estado: EstadoDesconhecido}
	if configDir == "" || sessionID == "" {
		return unknown
	}
	sf, ok := t.findLive(filepath.Join(configDir, "sessions"), sessionID)
	if !ok {
		return unknown
	}
	if sf.Tmux == "" {
		return Location{Estado: EstadoFora}
	}
	m := tmuxFieldRe.FindStringSubmatch(sf.Tmux)
	if m == nil {
		return unknown
	}
	pane := m[1]

	ctx, cancel := context.WithTimeout(ctx, locateTimeout)
	defer cancel()
	out, err := t.run.Run(ctx, "tmux", locateArgs(pane)...)
	if err != nil {
		return unknown
	}
	f := strings.Split(strings.TrimRight(out, "\n"), "\t")
	if len(f) != 4 || f[3] == "" {
		return unknown
	}
	// O %N só vale se o pane ainda for o terminal do processo: depois de o
	// servidor do tmux reiniciar, o mesmo ID pode ser de outro pane.
	tty, err := os.Readlink(filepath.Join(t.proc, strconv.Itoa(sf.PID), "fd", "0"))
	if err != nil || tty != f[3] {
		return unknown
	}
	if _, err := strconv.Atoi(f[1]); err != nil {
		return unknown
	}
	if _, err := strconv.Atoi(f[2]); err != nil {
		return unknown
	}
	return Location{
		Estado: EstadoTmux,
		Rotulo: "tmux " + cleanLabel(f[0]) + ":" + f[1] + "." + f[2],
		Pane:   pane,
	}
}

// findLive acha o arquivo da sessão cujo processo ainda é o mesmo que o
// gravou (procStart igual ao starttime do /proc/<pid>/stat).
func (t *Terminal) findLive(dir, sessionID string) (sessionFile, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return sessionFile{}, false
	}
	for _, e := range entries {
		m := sessionFileRe.FindStringSubmatch(e.Name())
		if m == nil || !e.Type().IsRegular() {
			continue
		}
		sf, err := readSessionFile(filepath.Join(dir, e.Name()))
		if err != nil || sf.SessionID != sessionID || strconv.Itoa(sf.PID) != m[1] {
			continue
		}
		if t.alive(sf) {
			return sf, true
		}
	}
	return sessionFile{}, false
}

func readSessionFile(path string) (sessionFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return sessionFile{}, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxSessionBytes+1))
	if err != nil {
		return sessionFile{}, err
	}
	if len(data) > maxSessionBytes {
		return sessionFile{}, errors.New("arquivo de sessão grande demais")
	}
	var sf sessionFile
	if err := json.Unmarshal(data, &sf); err != nil {
		return sessionFile{}, err
	}
	return sf, nil
}

// alive confere o procStart (string ou número no JSON) contra o campo 22 de
// /proc/<pid>/stat, o que descarta PID reaproveitado.
func (t *Terminal) alive(sf sessionFile) bool {
	if sf.PID <= 0 {
		return false
	}
	want := strings.Trim(string(sf.ProcStart), `"`)
	if want == "" {
		return false
	}
	stat, err := os.ReadFile(filepath.Join(t.proc, strconv.Itoa(sf.PID), "stat"))
	if err != nil {
		return false
	}
	// O nome do processo (campo 2) vem entre parênteses e pode ter espaços;
	// os campos seguintes começam depois do último ')', a partir do 3.
	i := strings.LastIndexByte(string(stat), ')')
	if i < 0 {
		return false
	}
	rest := strings.Fields(string(stat[i+1:]))
	const starttime = 22 - 3
	return len(rest) > starttime && rest[starttime] == want
}

// Open abre uma janela nova do Ptyxis presa ao pane de loc.
func (t *Terminal) Open(ctx context.Context, loc Location) error {
	if loc.Estado != EstadoTmux || !paneRe.MatchString(loc.Pane) {
		return errors.New("terminal: sessão fora do tmux")
	}
	ctx, cancel := context.WithTimeout(ctx, openTimeout)
	defer cancel()
	title := "claude-fleet · " + cleanLabel(loc.Rotulo)
	if _, err := t.run.Run(ctx, "systemd-run", openArgs(title, loc.Pane)...); err != nil {
		return fmt.Errorf("terminal: abrir janela: %w", err)
	}
	return nil
}

// cleanLabel tira caracteres de controle e limita o tamanho do texto que vem
// do tmux (nome da sessão).
func cleanLabel(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	if r := []rune(s); len(r) > maxRotuloRunes {
		s = string(r[:maxRotuloRunes])
	}
	return s
}
