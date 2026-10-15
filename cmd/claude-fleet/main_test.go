package main

import (
	"errors"
	"flag"
	"io"
	"strings"
	"testing"
)

func TestValidateBind(t *testing.T) {
	tests := []struct {
		name    string
		addr    string
		token   string
		wantErr bool
	}{
		{"loopback IPv4 padrão", "127.0.0.1:7777", "", false},
		{"loopback 127.x", "127.0.0.2:7777", "", false},
		{"loopback IPv6", "[::1]:7777", "", false},
		{"IPv4 mapeado em IPv6", "[::ffff:127.0.0.1]:7777", "", false},
		{"localhost", "localhost:7777", "", false},
		{"todas as interfaces sem token", "0.0.0.0:7777", "", true},
		{"todas as interfaces com token", "0.0.0.0:7777", "s3cr3t", false},
		{"host vazio sem token", ":7777", "", true},
		{"IPv6 todas as interfaces sem token", "[::]:7777", "", true},
		{"IP da tailnet sem token", "100.64.0.10:7777", "", true},
		{"IP da tailnet com token", "100.64.0.10:7777", "s3cr3t", false},
		{"token só com espaços", "0.0.0.0:7777", "   ", true},
		{"hostname não loopback sem token", "minha-maquina:7777", "", true},
		{"sem porta", "127.0.0.1", "", true},
		{"porta vazia", "127.0.0.1:", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateBind(tt.addr, tt.token)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateBind(%q, %q) erro = %v, queria erro = %t", tt.addr, tt.token, err, tt.wantErr)
			}
		})
	}
}

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestParseConfigDefaults(t *testing.T) {
	cfg, err := parseConfig(nil, env(map[string]string{"HOME": "/home/x"}), io.Discard)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if cfg.Addr != "127.0.0.1:7777" {
		t.Errorf("Addr = %q, queria 127.0.0.1:7777", cfg.Addr)
	}
	if cfg.Projects != "/home/x/.claude/projects" {
		t.Errorf("Projects = %q", cfg.Projects)
	}
	if cfg.Token != "" {
		t.Errorf("Token = %q, queria vazio", cfg.Token)
	}
}

func TestParseConfigTokenFromEnv(t *testing.T) {
	e := env(map[string]string{"HOME": "/home/x", "CLAUDE_FLEET_TOKEN": "do-ambiente"})
	cfg, err := parseConfig([]string{"-addr", "0.0.0.0:7777"}, e, io.Discard)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if cfg.Token != "do-ambiente" {
		t.Errorf("Token = %q, queria do-ambiente", cfg.Token)
	}

	cfg, err = parseConfig([]string{"-token", "da-flag"}, e, io.Discard)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if cfg.Token != "da-flag" {
		t.Errorf("flag deveria prevalecer sobre o ambiente; Token = %q", cfg.Token)
	}
}

func TestParseConfigRejectsExposedBindWithoutToken(t *testing.T) {
	_, err := parseConfig([]string{"-addr", "0.0.0.0:7777"}, env(map[string]string{"HOME": "/home/x"}), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "token") {
		t.Fatalf("queria erro mencionando token, veio %v", err)
	}
}

func TestParseConfigExplicitProjects(t *testing.T) {
	cfg, err := parseConfig([]string{"-projects", "/tmp/p"}, env(nil), io.Discard)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if cfg.Projects != "/tmp/p" {
		t.Errorf("Projects = %q", cfg.Projects)
	}
}

func TestParseConfigErrors(t *testing.T) {
	if _, err := parseConfig(nil, env(nil), io.Discard); err == nil {
		t.Error("sem HOME e sem -projects deveria falhar")
	}
	if _, err := parseConfig([]string{"extra"}, env(map[string]string{"HOME": "/h"}), io.Discard); err == nil {
		t.Error("argumento posicional deveria falhar")
	}
	if _, err := parseConfig([]string{"-h"}, env(map[string]string{"HOME": "/h"}), io.Discard); !errors.Is(err, flag.ErrHelp) {
		t.Errorf("-h deveria devolver flag.ErrHelp, veio %v", err)
	}
}

func TestListen(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:0", "0.0.0.0:0"} {
		lns, err := listen(addr)
		if err != nil {
			t.Fatalf("listen(%q): %v", addr, err)
		}
		if len(lns) != 1 {
			t.Errorf("listen(%q) abriu %d listeners, queria 1", addr, len(lns))
		}
		for _, ln := range lns {
			ln.Close()
		}
	}
}
