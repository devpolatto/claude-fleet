package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/devpolatto/claude-fleet/internal/terminal"
)

const (
	tokTerm   = "token-de-teste-terminal"
	hostTerm  = "100.64.0.1:7777" // IP do Tailscale da máquina
	ipLocal   = "100.64.0.1"
	ipCelular = "100.64.0.7"
	ipOutro   = "100.64.0.9"
)

// fakeTerminal devolve loc em Locate e err em Open, registrando as chamadas.
type fakeTerminal struct {
	mu        sync.Mutex
	loc       terminal.Location
	err       error
	configDir string
	opens     int
}

func (f *fakeTerminal) Locate(_ context.Context, configDir, _ string) terminal.Location {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.configDir = configDir
	return f.loc
}

func (f *fakeTerminal) Open(context.Context, terminal.Location) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.opens++
	return f.err
}

var locTmux = terminal.Location{Estado: terminal.EstadoTmux, Rotulo: "tmux 6:1.0", Pane: "%9"}

// servidorTerminal sobe o servidor com token, uma sessão conhecida e o
// terminal falso; o celular (ipCelular) está em -focus-from.
func servidorTerminal(t *testing.T, ft *fakeTerminal) (*Server, http.Handler) {
	t.Helper()
	_, srv, _ := novoServidor(t, tokTerm)
	srv.terminal.localAddrs = func() []netip.Addr {
		return []netip.Addr{netip.MustParseAddr(ipLocal), netip.MustParseAddr("192.168.20.5")}
	}
	srv.SetTerminal([]netip.Addr{netip.MustParseAddr(ipCelular)}, ft)
	h := srv.Handler()
	if rec := do(h, http.MethodPost, "/event", "127.0.0.1:1", eventoComConteudo); rec.Code != http.StatusNoContent {
		t.Fatalf("POST /event = %d", rec.Code)
	}
	return srv, h
}

// pedido monta a requisição como a página do painel a faz; mod ajusta.
func pedido(method, path, remote string, mod func(*http.Request)) *http.Request {
	body := ""
	if method == http.MethodPost {
		body = "{}"
	}
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Host = hostTerm
	req.RemoteAddr = remote
	req.AddCookie(&http.Cookie{Name: CookieName, Value: tokTerm})
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "https://"+hostTerm)
	}
	if mod != nil {
		mod(req)
	}
	return req
}

func serve(h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

const (
	rotaTerminal = "/api/session/sess-0123456789/terminal"
	rotaAbrir    = "/api/session/sess-0123456789/terminal/abrir"
)

func TestTrustedPeer(t *testing.T) {
	srv, _ := servidorTerminal(t, &fakeTerminal{})
	tests := []struct {
		remote string
		want   bool
	}{
		{"127.0.0.1:5000", true},
		{"[::1]:5000", true},
		{ipLocal + ":5000", true},
		{"192.168.20.5:5000", true},
		{ipCelular + ":5000", true},
		{"[::ffff:" + ipCelular + "]:5000", true},
		{ipOutro + ":5000", false},
		{"[fd7a:115c:a1e0::9]:5000", false},
		{"lixo", false},
	}
	for _, tt := range tests {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = tt.remote
		if got := srv.trustedPeer(req); got != tt.want {
			t.Errorf("trustedPeer(%s) = %v, quer %v", tt.remote, got, tt.want)
		}
	}
}

func TestTerminalPodeAbrirSoAutorizado(t *testing.T) {
	ft := &fakeTerminal{loc: locTmux}
	_, h := servidorTerminal(t, ft)
	for _, tt := range []struct {
		remote string
		pode   bool
	}{
		{ipLocal + ":5000", true},
		{ipCelular + ":5000", true},
		{ipOutro + ":5000", false},
	} {
		rec := serve(h, pedido(http.MethodGet, rotaTerminal, tt.remote, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d", tt.remote, rec.Code)
		}
		want := `{"estado":"tmux","rotulo":"tmux 6:1.0","podeAbrir":` + map[bool]string{true: "true", false: "false"}[tt.pode] + "}\n"
		if got := rec.Body.String(); got != want {
			t.Errorf("%s: corpo = %s, quer %s", tt.remote, got, want)
		}
	}
	if ft.configDir != "/home/x/.claude" {
		t.Errorf("configDir = %q, quer o diretório da conta deduzido do transcript", ft.configDir)
	}
}

func TestTerminalForaDoTmuxNaoPodeAbrir(t *testing.T) {
	_, h := servidorTerminal(t, &fakeTerminal{loc: terminal.Location{Estado: terminal.EstadoFora}})
	rec := serve(h, pedido(http.MethodGet, rotaTerminal, ipLocal+":5000", nil))
	if want := `{"estado":"fora","rotulo":"","podeAbrir":false}` + "\n"; rec.Body.String() != want {
		t.Errorf("corpo = %s, quer %s", rec.Body.String(), want)
	}
}

func TestTerminalSessaoDesconhecida404(t *testing.T) {
	_, h := servidorTerminal(t, &fakeTerminal{loc: locTmux})
	for _, req := range []*http.Request{
		pedido(http.MethodGet, "/api/session/outra/terminal", ipLocal+":5000", nil),
		pedido(http.MethodPost, "/api/session/outra/terminal/abrir", ipLocal+":5000", nil),
	} {
		if rec := serve(h, req); rec.Code != http.StatusNotFound {
			t.Errorf("%s %s: status = %d, quer 404", req.Method, req.URL.Path, rec.Code)
		}
	}
}

func TestTerminalSemSetTerminal404(t *testing.T) {
	_, _, h := novoServidor(t, tokTerm)
	do(h, http.MethodPost, "/event", "127.0.0.1:1", eventoComConteudo)
	for _, req := range []*http.Request{
		pedido(http.MethodGet, rotaTerminal, "127.0.0.1:5000", nil),
		pedido(http.MethodPost, rotaAbrir, "127.0.0.1:5000", nil),
	} {
		if rec := serve(h, req); rec.Code != http.StatusNotFound {
			t.Errorf("%s %s: status = %d, quer 404", req.Method, req.URL.Path, rec.Code)
		}
	}
}

func TestTerminalExigeToken(t *testing.T) {
	ft := &fakeTerminal{loc: locTmux}
	_, h := servidorTerminal(t, ft)
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		path := map[string]string{http.MethodGet: rotaTerminal, http.MethodPost: rotaAbrir}[method]
		req := pedido(method, path, ipLocal+":5000", nil)
		req.Header.Del("Cookie")
		if rec := serve(h, req); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s sem token: status = %d, quer 401", method, rec.Code)
		}
	}
	if ft.opens != 0 {
		t.Errorf("Open chamado %d vezes sem token", ft.opens)
	}
}

func TestAbrirRegrasDoPedido(t *testing.T) {
	tests := []struct {
		name   string
		remote string
		mod    func(*http.Request)
		want   int
	}{
		{"máquina", ipLocal + ":5000", nil, http.StatusNoContent},
		{"aceita IP de -focus-from", ipCelular + ":5000", nil, http.StatusNoContent},
		{"recusa outro aparelho", ipOutro + ":5000", nil, http.StatusForbidden},
		{"sem JSON", ipLocal + ":5000", func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }, http.StatusUnsupportedMediaType},
		{"sem same-origin", ipLocal + ":5000", func(r *http.Request) { r.Header.Del("Sec-Fetch-Site") }, http.StatusForbidden},
		{"same-site", ipLocal + ":5000", func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "same-site") }, http.StatusForbidden},
		{"sem Origin", ipLocal + ":5000", func(r *http.Request) { r.Header.Del("Origin") }, http.StatusForbidden},
		{"Origin de outro host", ipLocal + ":5000", func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") }, http.StatusForbidden},
		{"Origin de outra porta", ipLocal + ":5000", func(r *http.Request) { r.Header.Set("Origin", "https://100.64.0.1:8080") }, http.StatusForbidden},
		{"corpo inválido", ipLocal + ":5000", func(r *http.Request) { r.Body = http.NoBody }, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ft := &fakeTerminal{loc: locTmux}
			_, h := servidorTerminal(t, ft)
			rec := serve(h, pedido(http.MethodPost, rotaAbrir, tt.remote, tt.mod))
			if rec.Code != tt.want {
				t.Fatalf("status = %d, quer %d (%s)", rec.Code, tt.want, rec.Body.String())
			}
			if wantOpens := map[bool]int{true: 1, false: 0}[tt.want == http.StatusNoContent]; ft.opens != wantOpens {
				t.Errorf("Open chamado %d vezes, quer %d", ft.opens, wantOpens)
			}
		})
	}
}

func TestAbrirSegundoPedidoEm3sRecebe429(t *testing.T) {
	ft := &fakeTerminal{loc: locTmux}
	srv, h := servidorTerminal(t, ft)
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	srv.now = func() time.Time { return now }

	if rec := serve(h, pedido(http.MethodPost, rotaAbrir, ipLocal+":5000", nil)); rec.Code != http.StatusNoContent {
		t.Fatalf("1º: status = %d", rec.Code)
	}
	now = now.Add(2 * time.Second)
	if rec := serve(h, pedido(http.MethodPost, rotaAbrir, ipCelular+":5000", nil)); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("2º em 2 s: status = %d, quer 429", rec.Code)
	}
	now = now.Add(time.Second)
	if rec := serve(h, pedido(http.MethodPost, rotaAbrir, ipLocal+":5000", nil)); rec.Code != http.StatusNoContent {
		t.Fatalf("3º em 3 s: status = %d, quer 204", rec.Code)
	}
	if ft.opens != 2 {
		t.Errorf("Open chamado %d vezes, quer 2", ft.opens)
	}
}

func TestAbrirSessaoEncerrada409(t *testing.T) {
	for _, est := range []terminal.Estado{terminal.EstadoFora, terminal.EstadoDesconhecido} {
		ft := &fakeTerminal{loc: terminal.Location{Estado: est}}
		_, h := servidorTerminal(t, ft)
		if rec := serve(h, pedido(http.MethodPost, rotaAbrir, ipLocal+":5000", nil)); rec.Code != http.StatusConflict || ft.opens != 0 {
			t.Errorf("%s: status = %d, opens = %d; quer 409 sem abrir", est, rec.Code, ft.opens)
		}
	}
}

func TestAbrirFalhaDoOpen502(t *testing.T) {
	ft := &fakeTerminal{loc: locTmux, err: errors.New("systemd-run: exit status 1: Failed to start")}
	_, h := servidorTerminal(t, ft)
	rec := serve(h, pedido(http.MethodPost, rotaAbrir, ipLocal+":5000", nil))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, quer 502", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "systemd") {
		t.Errorf("a resposta expõe o motivo detalhado: %s", rec.Body.String())
	}
}
