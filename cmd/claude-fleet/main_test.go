package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/devpolatto/claude-fleet/internal/store"
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

func TestValidateTLS(t *testing.T) {
	ok := []struct{ addr, cert, key string }{
		{"127.0.0.1:7777", "", ""},
		{"100.75.1.2:7777", "c.crt", "c.key"},
	}
	for _, c := range ok {
		if err := validateTLS(c.addr, c.cert, c.key); err != nil {
			t.Errorf("validateTLS(%q, %q, %q) = %v", c.addr, c.cert, c.key, err)
		}
	}
	ruins := []struct{ addr, cert, key string }{
		{"100.75.1.2:7777", "c.crt", ""},
		{"100.75.1.2:7777", "", "c.key"},
		{"127.0.0.1:7777", "c.crt", "c.key"}, // o hook ficaria sem HTTP
		{"0.0.0.0:7777", "c.crt", "c.key"},   // idem: listen não abre o loopback à parte
		{":7777", "c.crt", "c.key"},
		{"meu-pc.ts.net:7777", "c.crt", "c.key"}, // nome: listen não abre o loopback
	}
	for _, c := range ruins {
		if err := validateTLS(c.addr, c.cert, c.key); err == nil {
			t.Errorf("validateTLS(%q, %q, %q) deveria falhar", c.addr, c.cert, c.key)
		}
	}
}

func TestParseConfigTLS(t *testing.T) {
	e := env(map[string]string{"HOME": "/h", "CLAUDE_FLEET_TOKEN": "tok"})
	cfg, err := parseConfig([]string{"-addr", "100.75.1.2:7777", "-tls-cert", "c.crt", "-tls-key", "c.key"}, e, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TLSCert != "c.crt" || cfg.TLSKey != "c.key" {
		t.Errorf("cfg = %+v", cfg)
	}
	if _, err := parseConfig([]string{"-tls-cert", "c.crt", "-tls-key", "c.key"}, e, io.Discard); err == nil {
		t.Error("TLS no endereço padrão (loopback) deveria falhar")
	}
}

// O listener TLS precisa negociar HTTP/2 mesmo com o loopback HTTP subindo
// junto: com um http.Server compartilhado, o h2 era fechado após o pedido.
func TestServeTLSComHTTP2ELoopbackHTTP(t *testing.T) {
	ref := httptest.NewTLSServer(nil) // só para emprestar certificado e CA
	cert := ref.TLS.Certificates[0]
	roots := ref.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs
	ref.Close()

	lt, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	lp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, r.Proto) })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- serve(ctx, h, &tls.Config{Certificates: []tls.Certificate{cert}}, []net.Listener{lt, lp})
	}()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("serve: %v", err)
		}
	})

	get := func(c *http.Client, url string) string {
		t.Helper()
		resp, err := c.Get(url)
		if err != nil {
			t.Fatalf("GET %s: %v", url, err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return string(b)
	}
	h2 := &http.Client{Transport: &http.Transport{
		TLSClientConfig:   &tls.Config{RootCAs: roots, ServerName: "example.com"},
		ForceAttemptHTTP2: true,
	}}
	for i := 0; i < 3; i++ {
		if got := get(h2, "https://"+lt.Addr().String()); got != "HTTP/2.0" {
			t.Errorf("TLS: proto = %q, queria HTTP/2.0", got)
		}
	}
	if got := get(http.DefaultClient, "http://"+lp.Addr().String()); got != "HTTP/1.1" {
		t.Errorf("loopback: proto = %q, queria HTTP/1.1", got)
	}
}

func TestParseConfigAccountComProjectsFalha(t *testing.T) {
	e := env(map[string]string{"HOME": "/home/x"})
	_, err := parseConfig([]string{"-account", "p=~/.claude-p", "-projects", "/tmp/p"}, e, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "use só -account") {
		t.Fatalf("erro = %v, queria a mensagem de -account com -projects", err)
	}

	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{"nome inválido", []string{"-account", "P=/c"}, "-account"},
		{"sem igual", []string{"-account", "pessoal"}, "nome=diretório"},
		{"nome repetido", []string{"-account", "p=/a", "-account", "p=/b"}, "repetida"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseConfig(tt.args, e, io.Discard)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("erro = %v, queria conter %q", err, tt.wantErr)
			}
		})
	}
}

func TestParseConfigAccountValido(t *testing.T) {
	cfg, err := parseConfig([]string{"-account", "pessoal=~/.claude-pessoal", "-account", "trabalho=/srv/c"},
		env(map[string]string{"HOME": "/home/x"}), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Projects != "" {
		t.Errorf("Projects = %q, queria vazio com -account", cfg.Projects)
	}
	all := cfg.Accounts.All()
	if len(all) != 2 || all[0].Name != "pessoal" || all[0].Dir != "/home/x/.claude-pessoal" || all[1].Dir != "/srv/c" {
		t.Errorf("Accounts = %+v", all)
	}

	cfg, err = parseConfig(nil, env(map[string]string{"HOME": "/home/x"}), io.Discard)
	if err != nil || cfg.Accounts != nil || cfg.Projects != "/home/x/.claude/projects" {
		t.Errorf("sem -account: Accounts=%v Projects=%q err=%v", cfg.Accounts, cfg.Projects, err)
	}
}

func TestBootstrapPorConta(t *testing.T) {
	home := t.TempDir()
	linha := `{"type":"user","cwd":"/home/u/proj","timestamp":"2026-10-18T10:00:00.000Z"}` + "\n"
	for _, c := range []string{"pessoal", "trabalho"} {
		p := filepath.Join(home, ".claude-"+c, "projects", "-home-u-proj", "s-"+c+".jsonl")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(linha), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := parseConfig([]string{"-account", "pessoal=~/.claude-pessoal", "-account", "trabalho=~/.claude-trabalho",
		"-account", "vazia=~/.claude-vazia"}, env(map[string]string{"HOME": home}), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	st := store.New(store.WithAccounts(cfg.Accounts))
	bootstrap(st, cfg, time.Now())

	for _, c := range []string{"pessoal", "trabalho"} {
		d, ok := st.Get("s-" + c)
		if !ok {
			t.Fatalf("sessão de %s não descoberta", c)
		}
		if d.Account != c {
			t.Errorf("s-%s: Account = %q, queria %q", c, d.Account, c)
		}
	}
	if n := len(st.Snapshot()); n != 2 {
		t.Errorf("%d sessões, queria 2", n)
	}
}
