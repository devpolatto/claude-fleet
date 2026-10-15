// Command claude-fleet serve o painel de sessões e subagentes do Claude Code.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
)

const defaultAddr = "127.0.0.1:7777"

type config struct {
	Addr     string
	Token    string
	Projects string
}

func main() {
	cfg, err := parseConfig(os.Args[1:], os.Getenv, os.Stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		log.Fatalf("claude-fleet: %v", err)
	}
	// O servidor HTTP entra no T11; por enquanto só valida e mostra a configuração.
	log.Printf("claude-fleet: addr=%s projects=%s token=%t", cfg.Addr, cfg.Projects, cfg.Token != "")
}

// parseConfig lê as flags e o ambiente e recusa um bind fora do loopback sem token.
func parseConfig(args []string, getenv func(string) string, out io.Writer) (config, error) {
	fs := flag.NewFlagSet("claude-fleet", flag.ContinueOnError)
	fs.SetOutput(out)

	cfg := config{}
	fs.StringVar(&cfg.Addr, "addr", defaultAddr, "endereço de escuta (host:porta)")
	fs.StringVar(&cfg.Token, "token", getenv("CLAUDE_FLEET_TOKEN"), "token de acesso; obrigatório fora do loopback (padrão: $CLAUDE_FLEET_TOKEN)")
	fs.StringVar(&cfg.Projects, "projects", "", "diretório dos transcripts (padrão: ~/.claude/projects)")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	if fs.NArg() > 0 {
		return config{}, fmt.Errorf("argumento inesperado: %q", fs.Arg(0))
	}

	if cfg.Projects == "" {
		home := getenv("HOME")
		if home == "" {
			return config{}, errors.New("HOME não definido; informe -projects")
		}
		cfg.Projects = filepath.Join(home, ".claude", "projects")
	}

	if err := validateBind(cfg.Addr, cfg.Token); err != nil {
		return config{}, err
	}
	return cfg, nil
}

// validateBind exige token quando addr não é exclusivamente loopback.
// Host vazio (":7777") escuta em todas as interfaces e por isso também exige token.
func validateBind(addr, token string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("endereço inválido %q: %w", addr, err)
	}
	if port == "" {
		return fmt.Errorf("endereço inválido %q: porta ausente", addr)
	}
	if isLoopback(host) {
		return nil
	}
	if strings.TrimSpace(token) == "" {
		return fmt.Errorf("bind em %q expõe o painel fora do loopback; defina -token ou CLAUDE_FLEET_TOKEN", addr)
	}
	return nil
}

func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	return ip.Unmap().IsLoopback()
}
