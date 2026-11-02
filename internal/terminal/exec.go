package terminal

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"unicode"
)

// Runner executa um programa e devolve a saída padrão. Existe para os testes
// trocarem o tmux e o systemd por um falso.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) (string, error)
}

// locateFormat é o formato do display-message: sessão, janela, pane e tty.
const locateFormat = "#{session_name}\t#{window_index}\t#{pane_index}\t#{pane_tty}"

// maxOutput limita o que se lê de stdout e stderr de cada comando.
const maxOutput = 4 << 10

// paneRe valida o ID de pane do tmux (%N), o único dado variável que chega
// aos comandos além do título.
var paneRe = regexp.MustCompile(`^%[0-9]{1,9}$`)

// ErrNotAllowed é devolvido para qualquer comando fora da lista fechada.
var ErrNotAllowed = errors.New("terminal: comando fora da lista permitida")

// locateArgs monta o display-message que lê onde o pane está.
func locateArgs(pane string) []string {
	return []string{"display-message", "-p", "-t", pane, locateFormat}
}

// openArgs monta o systemd-run que abre uma janela nova do Ptyxis presa ao
// pane. O attach-session com um ID de pane já seleciona a janela e o pane
// (verificado no tmux 3.6). Não há shell em nenhum ponto.
func openArgs(title, pane string) []string {
	return []string{
		"--user", "--quiet", "--collect",
		"ptyxis", "--new-window", "-T", title,
		"--", "tmux", "attach-session", "-t", pane,
	}
}

// allowed confere que name e args são exatamente um dos dois comandos da
// AD-010, com pane válido e título sem caractere de controle.
func allowed(name string, args []string) error {
	var want []string
	switch name {
	case "tmux":
		if len(args) != 5 || !paneRe.MatchString(args[3]) {
			return ErrNotAllowed
		}
		want = locateArgs(args[3])
	case "systemd-run":
		if len(args) != 12 || !paneRe.MatchString(args[11]) || !cleanTitle(args[6]) {
			return ErrNotAllowed
		}
		want = openArgs(args[6], args[11])
	default:
		return ErrNotAllowed
	}
	for i := range want {
		if args[i] != want[i] {
			return ErrNotAllowed
		}
	}
	return nil
}

func cleanTitle(s string) bool {
	if s == "" || len(s) > 200 {
		return false
	}
	return strings.IndexFunc(s, unicode.IsControl) < 0
}

// execRunner é o Runner real: recusa tudo fora da lista fechada antes de
// executar.
type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	if err := allowed(name, args); err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, name, args...)
	var out, errOut limited
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(errOut.String()); msg != "" {
			return "", fmt.Errorf("%s: %w: %s", name, err, firstLine(msg))
		}
		return "", fmt.Errorf("%s: %w", name, err)
	}
	return out.String(), nil
}

// limited guarda até maxOutput bytes e descarta o resto sem erro.
type limited struct{ bytes.Buffer }

func (l *limited) Write(p []byte) (int, error) {
	if room := maxOutput - l.Len(); room > 0 {
		if len(p) > room {
			l.Buffer.Write(p[:room])
		} else {
			l.Buffer.Write(p)
		}
	}
	return len(p), nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}
