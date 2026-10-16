package web

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/devpolatto/claude-fleet/internal/store"
)

const sentinela = "SENTINELA-CONTEUDO"

const eventoComConteudo = `{
  "hook_event_name": "UserPromptSubmit",
  "session_id": "sess-0123456789",
  "cwd": "/home/x/projeto",
  "transcript_path": "/home/x/.claude/projects/p/sess-0123456789.jsonl",
  "permission_mode": "default",
  "prompt": "` + sentinela + `",
  "tool_input": {"command": "` + sentinela + `"},
  "last_assistant_message": "` + sentinela + `"
}`

// novoServidor cria Store, Hub e Server; o Hub roda até o fim do teste.
func novoServidor(t *testing.T, token string) (*store.Store, *Server, http.Handler) {
	t.Helper()
	st := store.New()
	hub := NewHub(st)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go hub.Run(ctx)
	srv := NewServer(st, hub, token)
	return st, srv, srv.Handler()
}

func do(h http.Handler, method, target, remote, body string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if remote != "" {
		req.RemoteAddr = remote
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestEventSoAceitaLoopback(t *testing.T) {
	tests := []struct {
		name   string
		remote string
		want   int
	}{
		{"IPv4 loopback", "127.0.0.1:40000", http.StatusNoContent},
		{"IPv6 loopback", "[::1]:40000", http.StatusNoContent},
		{"IP da tailnet", "100.64.0.10:40000", http.StatusForbidden},
		{"rede local", "192.168.0.20:40000", http.StatusForbidden},
		{"RemoteAddr inválido", "lixo", http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st, _, h := novoServidor(t, "")
			rec := do(h, http.MethodPost, "/event", tt.remote, eventoComConteudo)
			if rec.Code != tt.want {
				t.Fatalf("status = %d, queria %d", rec.Code, tt.want)
			}
			_, ok := st.Get("sess-0123456789")
			if ok != (tt.want == http.StatusNoContent) {
				t.Errorf("sessão criada = %t, queria %t", ok, tt.want == http.StatusNoContent)
			}
		})
	}
}

func TestEventPayloadInvalido(t *testing.T) {
	_, srv, h := novoServidor(t, "")
	if rec := do(h, http.MethodPost, "/event", "127.0.0.1:1", `{"session_id":"x"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("sem hook_event_name: status = %d, queria 400", rec.Code)
	}
	if rec := do(h, http.MethodPost, "/event", "127.0.0.1:1", `{`+sentinela); rec.Code != http.StatusBadRequest || strings.Contains(rec.Body.String(), sentinela) {
		t.Errorf("JSON inválido: status = %d, corpo = %q", rec.Code, rec.Body.String())
	}
	grande := `{"hook_event_name":"Stop","session_id":"x","prompt":"` + strings.Repeat("a", MaxEventBytes) + `"}`
	if rec := do(h, http.MethodPost, "/event", "127.0.0.1:1", grande); rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("corpo grande: status = %d, queria 413", rec.Code)
	}
	if n := srv.EventErrors(); n != 3 {
		t.Errorf("EventErrors = %d, queria 3", n)
	}
	if rec := do(h, http.MethodGet, "/event", "127.0.0.1:1", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /event: status = %d, queria 405", rec.Code)
	}
}

func TestSemTokenRotasAbertas(t *testing.T) {
	_, _, h := novoServidor(t, "")
	for _, path := range []string{"/", "/api/sessions"} {
		if rec := do(h, http.MethodGet, path, "127.0.0.1:1", ""); rec.Code != http.StatusOK {
			t.Errorf("GET %s: status = %d, queria 200", path, rec.Code)
		}
	}
}

func TestComToken(t *testing.T) {
	const token = "s3cr3t-token"
	_, _, h := novoServidor(t, token)

	t.Run("sem credencial", func(t *testing.T) {
		for _, path := range []string{"/", "/stream", "/api/sessions", "/api/session/x"} {
			if rec := do(h, http.MethodGet, path, "100.64.0.10:1", ""); rec.Code != http.StatusUnauthorized {
				t.Errorf("GET %s: status = %d, queria 401", path, rec.Code)
			}
		}
	})
	t.Run("token errado", func(t *testing.T) {
		if rec := do(h, http.MethodGet, "/?t=errado", "100.64.0.10:1", ""); rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, queria 401", rec.Code)
		}
		bad := &http.Cookie{Name: CookieName, Value: "errado"}
		if rec := do(h, http.MethodGet, "/", "100.64.0.10:1", "", bad); rec.Code != http.StatusUnauthorized {
			t.Errorf("cookie errado: status = %d, queria 401", rec.Code)
		}
	})

	var cookie *http.Cookie
	t.Run("?t= grava cookie e limpa a URL", func(t *testing.T) {
		rec := do(h, http.MethodGet, "/?t="+token, "100.64.0.10:1", "")
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" {
			t.Fatalf("status = %d, Location = %q; queria 303 para /", rec.Code, rec.Header().Get("Location"))
		}
		cookies := rec.Result().Cookies()
		if len(cookies) != 1 {
			t.Fatalf("cookies = %d, queria 1", len(cookies))
		}
		cookie = cookies[0]
		if cookie.Name != CookieName || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" {
			t.Errorf("cookie = %+v, queria HttpOnly, SameSite=Lax e Path=/", cookie)
		}
	})
	t.Run("cookie autentica", func(t *testing.T) {
		if cookie == nil {
			t.Skip("sem cookie do passo anterior")
		}
		if rec := do(h, http.MethodGet, "/api/sessions", "100.64.0.10:1", "", cookie); rec.Code != http.StatusOK {
			t.Errorf("status = %d, queria 200", rec.Code)
		}
	})
	t.Run("/event dispensa token no loopback", func(t *testing.T) {
		if rec := do(h, http.MethodPost, "/event", "127.0.0.1:1", eventoComConteudo); rec.Code != http.StatusNoContent {
			t.Errorf("status = %d, queria 204", rec.Code)
		}
	})
}

func TestApiSession(t *testing.T) {
	_, _, h := novoServidor(t, "")
	if rec := do(h, http.MethodGet, "/api/session/inexistente", "127.0.0.1:1", ""); rec.Code != http.StatusNotFound {
		t.Errorf("inexistente: status = %d, queria 404", rec.Code)
	}
	do(h, http.MethodPost, "/event", "127.0.0.1:1", eventoComConteudo)
	rec := do(h, http.MethodGet, "/api/session/sess-0123456789", "127.0.0.1:1", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"Project":"projeto"`) {
		t.Errorf("existente: status = %d, corpo = %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}
}

// Nenhuma rota de leitura devolve o conteúdo enviado em /event.
func TestNenhumaRespostaVazaConteudo(t *testing.T) {
	_, _, h := novoServidor(t, "")
	ts := httptest.NewServer(h)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/event", "application/json", strings.NewReader(eventoComConteudo))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("POST /event: status = %d", resp.StatusCode)
	}

	for _, path := range []string{"/", "/api/sessions", "/api/session/sess-0123456789"} {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if strings.Contains(string(body), sentinela) {
			t.Errorf("GET %s vazou o conteúdo: %s", path, body)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/stream", nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "data:") {
			if strings.Contains(line, sentinela) {
				t.Errorf("/stream vazou o conteúdo: %s", line)
			}
			if !strings.Contains(line, "sess-0123456789") {
				t.Errorf("snapshot sem a sessão: %s", line)
			}
			return
		}
	}
	t.Fatal("nenhum snapshot recebido em /stream")
}

func TestApiNaoExpoeCaminhoDoTranscript(t *testing.T) {
	_, _, h := novoServidor(t, "")
	do(h, http.MethodPost, "/event", "127.0.0.1:1", eventoComConteudo)
	for _, path := range []string{"/api/sessions", "/api/session/sess-0123456789"} {
		body := do(h, http.MethodGet, path, "127.0.0.1:1", "").Body.String()
		if strings.Contains(body, "TranscriptPath") || strings.Contains(body, ".jsonl") {
			t.Errorf("GET %s expõe o caminho do transcript: %s", path, body)
		}
	}
}

// Manifesto e ícones saem sem token: o navegador os busca sem cookie.
func TestAssetsPublicos(t *testing.T) {
	_, _, h := novoServidor(t, "s3cr3t-token")
	for path, a := range publicAssets {
		rec := do(h, http.MethodGet, path, "100.64.0.10:1", "")
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s: status = %d, queria 200", path, rec.Code)
			continue
		}
		if got := rec.Header().Get("Content-Type"); got != a.ctype {
			t.Errorf("GET %s: Content-Type = %q, queria %q", path, got, a.ctype)
		}
		if rec.Body.Len() == 0 {
			t.Errorf("GET %s: corpo vazio", path)
		}
	}
	if rec := do(h, http.MethodGet, "/login.html", "100.64.0.10:1", ""); rec.Code == http.StatusOK {
		t.Error("rota fora da lista não deveria existir")
	}
}

func TestManifestoValido(t *testing.T) {
	_, _, h := novoServidor(t, "")
	rec := do(h, http.MethodGet, "/manifest.webmanifest", "127.0.0.1:1", "")
	var m struct {
		Name, StartURL, Display string
		Icons                   []struct{ Src, Sizes, Purpose string }
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("manifesto não é JSON: %v", err)
	}
	if m.Name != "Claude Fleet" || m.Display != "standalone" {
		t.Errorf("manifesto = %+v", m)
	}
	for _, ic := range m.Icons {
		if r := do(h, http.MethodGet, ic.Src, "127.0.0.1:1", ""); r.Code != http.StatusOK {
			t.Errorf("ícone %s do manifesto: status %d", ic.Src, r.Code)
		}
	}
}

func TestCookieSecureSoEmHTTPS(t *testing.T) {
	const token = "s3cr3t-token"
	_, _, h := novoServidor(t, token)
	for _, https := range []bool{false, true} {
		req := httptest.NewRequest(http.MethodGet, "/?t="+token, nil)
		req.RemoteAddr = "100.64.0.10:1"
		if https {
			req.TLS = &tls.ConnectionState{}
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		cookies := rec.Result().Cookies()
		if len(cookies) != 1 {
			t.Fatalf("https=%v: cookies = %d", https, len(cookies))
		}
		if cookies[0].Secure != https {
			t.Errorf("https=%v: Secure = %v", https, cookies[0].Secure)
		}
	}
}

func TestRedirectPreservaOutrosParametros(t *testing.T) {
	const token = "s3cr3t-token"
	_, _, h := novoServidor(t, token)
	rec := do(h, http.MethodGet, "/?x=1&t="+token, "100.64.0.10:1", "")
	if loc := rec.Header().Get("Location"); rec.Code != http.StatusSeeOther || loc != "/?x=1" {
		t.Errorf("status = %d, Location = %q; queria 303 para /?x=1", rec.Code, loc)
	}
}

// Fora da página, ?t= autentica sem redirecionar: SSE e API não seguem 303.
func TestTokenNaQueryDaAPISemRedirect(t *testing.T) {
	const token = "s3cr3t-token"
	_, _, h := novoServidor(t, token)
	if rec := do(h, http.MethodGet, "/api/sessions?t="+token, "100.64.0.10:1", ""); rec.Code != http.StatusOK {
		t.Errorf("status = %d, queria 200", rec.Code)
	}
}

func TestPaginaSemCredencialMostraLogin(t *testing.T) {
	const token = "s3cr3t-token"
	_, _, h := novoServidor(t, token)
	for _, target := range []string{"/", "/?t=errado"} {
		rec := do(h, http.MethodGet, target, "100.64.0.10:1", "")
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("GET %s: status = %d, queria 401", target, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Errorf("GET %s: Content-Type = %q, queria a tela de login", target, ct)
		}
		body := rec.Body.String()
		if !strings.Contains(body, `action="/login"`) || strings.Contains(body, token) {
			t.Errorf("GET %s: tela de login inesperada", target)
		}
		if got := strings.Contains(body, "Token inválido"); got != (target != "/") {
			t.Errorf("GET %s: aviso de token inválido = %v", target, got)
		}
	}
	if rec := do(h, http.MethodGet, "/api/sessions", "100.64.0.10:1", ""); strings.Contains(rec.Body.String(), "<form") {
		t.Error("a API não deveria devolver a tela de login")
	}
}

func TestLogin(t *testing.T) {
	const token = "s3cr3t-token"
	_, _, h := novoServidor(t, token)
	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.RemoteAddr = "100.64.0.10:1"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	rec := post("token=" + token)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" {
		t.Fatalf("token certo: status = %d, Location = %q", rec.Code, rec.Header().Get("Location"))
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != CookieName {
		t.Fatalf("token certo: cookies = %+v", cookies)
	}
	if r := do(h, http.MethodGet, "/api/sessions", "100.64.0.10:1", "", cookies[0]); r.Code != http.StatusOK {
		t.Errorf("cookie do login: status = %d", r.Code)
	}

	rec = post("token=errado")
	if rec.Code != http.StatusUnauthorized || len(rec.Result().Cookies()) != 0 || !strings.Contains(rec.Body.String(), "Token inválido") {
		t.Errorf("token errado: status = %d, cookies = %d", rec.Code, len(rec.Result().Cookies()))
	}
	if rec := post("token=" + strings.Repeat("x", MaxLoginBytes)); rec.Code != http.StatusBadRequest {
		t.Errorf("corpo grande: status = %d, queria 400", rec.Code)
	}
}

// A validade do cookie conta do último uso: a página o regrava; a API não.
func TestCookieRenovadoNaPagina(t *testing.T) {
	const token = "s3cr3t-token"
	_, _, h := novoServidor(t, token)
	c := &http.Cookie{Name: CookieName, Value: token}
	if rec := do(h, http.MethodGet, "/", "100.64.0.10:1", "", c); rec.Code != http.StatusOK || len(rec.Result().Cookies()) != 1 {
		t.Errorf("página: status = %d, cookies = %d; queria 200 e cookie renovado", rec.Code, len(rec.Result().Cookies()))
	}
	if rec := do(h, http.MethodGet, "/api/sessions", "100.64.0.10:1", "", c); len(rec.Result().Cookies()) != 0 {
		t.Error("a API não deveria regravar o cookie")
	}
}

// O service worker atualiza logo e não guarda nada no aparelho.
func TestServiceWorker(t *testing.T) {
	_, _, h := novoServidor(t, "s3cr3t-token")
	rec := do(h, http.MethodGet, "/sw.js", "100.64.0.10:1", "")
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("status = %d, Cache-Control = %q; queria 200 e no-cache", rec.Code, rec.Header().Get("Cache-Control"))
	}
	body := rec.Body.String()
	for _, proibido := range []string{"caches.", "'fetch'", `"fetch"`} {
		if strings.Contains(body, proibido) {
			t.Errorf("sw.js usa %s: o service worker não deve interceptar nem guardar requisições", proibido)
		}
	}
}
