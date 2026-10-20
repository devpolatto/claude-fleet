package web

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/devpolatto/claude-fleet/internal/account"
	"github.com/devpolatto/claude-fleet/internal/push"
	"github.com/devpolatto/claude-fleet/internal/ratelimit"
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

// do monta a requisição como o hook e o navegador do dono a mandam: Host de
// loopback e, com corpo, Content-Type application/json.
func do(h http.Handler, method, target, remote, body string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Host = "127.0.0.1:7777"
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
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

// O navegador do dono também é peer de loopback: um POST cross-site "simples"
// (text/plain, no-cors) ou qualquer requisição com Origin ou Sec-Fetch-Site de
// outro site não pode criar sessão nem gravar uso do plano.
func TestIngestRecusaRequisicaoDeNavegador(t *testing.T) {
	tests := []struct {
		name    string
		headers map[string]string
		want    int
	}{
		{"hook (curl)", map[string]string{"Content-Type": "application/json"}, http.StatusNoContent},
		{"Sec-Fetch-Site none", map[string]string{"Content-Type": "application/json", "Sec-Fetch-Site": "none"}, http.StatusNoContent},
		{"sem Content-Type", nil, http.StatusUnsupportedMediaType},
		{"text/plain no-cors", map[string]string{"Content-Type": "text/plain;charset=UTF-8"}, http.StatusUnsupportedMediaType},
		{"form", map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, http.StatusUnsupportedMediaType},
		{"text/plain com Origin", map[string]string{"Content-Type": "text/plain", "Origin": "https://mal.example"}, http.StatusForbidden},
		{"JSON com Origin", map[string]string{"Content-Type": "application/json", "Origin": "https://mal.example"}, http.StatusForbidden},
		{"Origin null", map[string]string{"Content-Type": "application/json", "Origin": "null"}, http.StatusForbidden},
		{"Sec-Fetch-Site cross-site", map[string]string{"Content-Type": "application/json", "Sec-Fetch-Site": "cross-site"}, http.StatusForbidden},
		{"Sec-Fetch-Site same-origin", map[string]string{"Content-Type": "application/json", "Sec-Fetch-Site": "same-origin"}, http.StatusForbidden},
	}
	for _, tt := range tests {
		for _, token := range []string{"", "tok"} {
			t.Run(tt.name+"/token="+token, func(t *testing.T) {
				st, srv, _ := novoServidor(t, token)
				u := &usoFalso{}
				srv.SetUsage(u, nil)
				h := srv.Handler()
				for _, rota := range []struct{ path, body string }{
					{"/event", eventoComConteudo},
					{"/statusline", statusline("/home/usuario/.claude/projects/p/a.jsonl")},
				} {
					req := httptest.NewRequest(http.MethodPost, rota.path, strings.NewReader(rota.body))
					req.RemoteAddr = "127.0.0.1:40000"
					req.Host = "127.0.0.1:7777"
					for k, v := range tt.headers {
						req.Header.Set(k, v)
					}
					rec := httptest.NewRecorder()
					h.ServeHTTP(rec, req)
					if rec.Code != tt.want {
						t.Errorf("POST %s: status = %d, queria %d", rota.path, rec.Code, tt.want)
					}
				}
				aceito := tt.want == http.StatusNoContent
				if _, ok := st.Get("sess-0123456789"); ok != aceito {
					t.Errorf("sessão criada = %t, queria %t", ok, aceito)
				}
				if aplicou := len(u.contas) == 1; aplicou != aceito {
					t.Errorf("uso gravado = %t, queria %t", aplicou, aceito)
				}
			})
		}
	}
}

// Sem token, só Host de loopback passa: um domínio com DNS rebinding para
// 127.0.0.1 não lê as rotas abertas nem manda evento. Com token, o Host não
// importa (o acesso pela tailnet usa o nome da máquina).
func TestSemTokenSoHostDeLoopback(t *testing.T) {
	tests := []struct {
		name  string
		token string
		host  string
		want  int
	}{
		{"127.0.0.1 com porta", "", "127.0.0.1:7777", http.StatusOK},
		{"localhost", "", "localhost:7777", http.StatusOK},
		{"LOCALHOST sem porta", "", "LOCALHOST", http.StatusOK},
		{"IPv6", "", "[::1]:7777", http.StatusOK},
		{"outro 127/8", "", "127.0.0.2:7777", http.StatusOK},
		{"domínio rebinding", "", "mal.example:7777", http.StatusMisdirectedRequest},
		{"subdomínio de localhost", "", "localhost.mal.example", http.StatusMisdirectedRequest},
		{"IP da LAN", "", "192.168.0.20:7777", http.StatusMisdirectedRequest},
		{"Host vazio", "", "", http.StatusMisdirectedRequest},
		{"com token, nome da tailnet", "tok", "maquina.tailnet.ts.net:7777", http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, h := novoServidor(t, tt.token)
			for _, path := range []string{"/api/sessions", "/manifest.webmanifest"} {
				target := path
				if tt.token != "" {
					target += "?t=" + tt.token
				}
				req := httptest.NewRequest(http.MethodGet, target, nil)
				req.RemoteAddr = "127.0.0.1:40000"
				req.Host = tt.host
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, req)
				if rec.Code != tt.want {
					t.Errorf("GET %s: status = %d, queria %d", path, rec.Code, tt.want)
				}
			}
		})
	}
	t.Run("POST /event com Host de rebinding", func(t *testing.T) {
		st, _, h := novoServidor(t, "")
		req := httptest.NewRequest(http.MethodPost, "/event", strings.NewReader(eventoComConteudo))
		req.RemoteAddr = "127.0.0.1:40000"
		req.Host = "mal.example:7777"
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusMisdirectedRequest {
			t.Errorf("status = %d, queria 421", rec.Code)
		}
		if _, ok := st.Get("sess-0123456789"); ok {
			t.Error("sessão criada com Host de rebinding")
		}
	})
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

// Token errado conta por IP: passadas maxTokenFailures falhas na janela, o IP
// recebe 429 até ela acabar, mesmo com o token certo; outro IP não é afetado
// e pedido sem credencial não conta.
func TestTokenErradoLimitadoPorIP(t *testing.T) {
	const token = "s3cr3t-token"
	agora := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	tentativas := map[string]func(h http.Handler, remote, tok string) *httptest.ResponseRecorder{
		"?t=": func(h http.Handler, remote, tok string) *httptest.ResponseRecorder {
			return do(h, http.MethodGet, "/api/sessions?t="+tok, remote, "")
		},
		"cookie": func(h http.Handler, remote, tok string) *httptest.ResponseRecorder {
			return do(h, http.MethodGet, "/api/sessions", remote, "", &http.Cookie{Name: CookieName, Value: tok})
		},
		"login": func(h http.Handler, remote, tok string) *httptest.ResponseRecorder {
			req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("token="+tok))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.RemoteAddr = remote
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			return rec
		},
	}
	for nome, tentar := range tentativas {
		t.Run(nome, func(t *testing.T) {
			_, srv, _ := novoServidor(t, token)
			srv.now = func() time.Time { return agora }
			h := srv.Handler()
			const ip = "100.64.0.10:1"

			for i := 0; i < 20; i++ {
				do(h, http.MethodGet, "/api/sessions", ip, "") // sem credencial: não conta
			}
			for i := 0; i < maxTokenFailures; i++ {
				if rec := tentar(h, ip, "errado"); rec.Code != http.StatusUnauthorized {
					t.Fatalf("tentativa %d: status = %d, queria 401", i+1, rec.Code)
				}
			}
			rec := tentar(h, ip, token)
			if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
				t.Fatalf("depois de %d falhas: status = %d, Retry-After = %q; queria 429", maxTokenFailures, rec.Code, rec.Header().Get("Retry-After"))
			}
			if rec := tentar(h, "100.64.0.11:1", token); rec.Code == http.StatusTooManyRequests || rec.Code == http.StatusUnauthorized {
				t.Errorf("outro IP: status = %d", rec.Code)
			}
			agora = agora.Add(failureWindow)
			if rec := tentar(h, ip, token); rec.Code == http.StatusTooManyRequests || rec.Code == http.StatusUnauthorized {
				t.Errorf("depois da janela: status = %d", rec.Code)
			}
		})
	}
}

func TestFailuresDescartaJanelasVencidas(t *testing.T) {
	var f failures
	t0 := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	for i := 0; i < maxTrackedIPs; i++ {
		f.add("10.0."+strconv.Itoa(i/256)+"."+strconv.Itoa(i%256), t0)
	}
	f.add("100.64.0.1", t0.Add(failureWindow))
	if n := len(f.m); n != 1 {
		t.Errorf("IPs no mapa = %d, queria 1", n)
	}
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

type pushFalso struct {
	subs    map[string]push.Subscription
	enviada []push.Message
}

func (p *pushFalso) PublicKey() string { return "CHAVE-PUBLICA" }
func (p *pushFalso) Subscribe(s push.Subscription) error {
	p.subs[s.Endpoint] = s
	return nil
}
func (p *pushFalso) Unsubscribe(endpoint string) error {
	delete(p.subs, endpoint)
	return nil
}
func (p *pushFalso) SendAll(_ context.Context, m push.Message, _ string) int {
	p.enviada = append(p.enviada, m)
	return len(p.subs)
}

const inscricaoValida = `{"endpoint":"https://fcm.googleapis.com/fcm/send/abc","expirationTime":null,"keys":{"p256dh":"BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4","auth":"BTBZMqHH6r4Tts7J_aSIgg"}}`

func reqJSON(h http.Handler, method, target, body string, cookie *http.Cookie, ctype string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.RemoteAddr = "100.64.0.10:1"
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestPushDesligado(t *testing.T) {
	_, _, h := novoServidor(t, "")
	if rec := do(h, http.MethodGet, "/api/push/key", "127.0.0.1:1", ""); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, queria 503", rec.Code)
	}
}

func TestPushAPI(t *testing.T) {
	const token = "s3cr3t-token"
	st := store.New()
	hub := NewHub(st)
	srv := NewServer(st, hub, token)
	pf := &pushFalso{subs: map[string]push.Subscription{}}
	srv.SetPush(pf)
	h := srv.Handler()
	c := &http.Cookie{Name: CookieName, Value: token}

	if rec := reqJSON(h, http.MethodGet, "/api/push/key", "", nil, ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("sem cookie: status = %d, queria 401", rec.Code)
	}
	if rec := reqJSON(h, http.MethodPost, "/api/push/subscription", inscricaoValida, nil, "application/json"); rec.Code != http.StatusUnauthorized {
		t.Errorf("inscrição sem cookie: status = %d, queria 401", rec.Code)
	}
	rec := reqJSON(h, http.MethodGet, "/api/push/key", "", c, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "CHAVE-PUBLICA") {
		t.Errorf("chave: %d %s", rec.Code, rec.Body)
	}

	if rec := reqJSON(h, http.MethodPost, "/api/push/subscription", inscricaoValida, c, "text/plain"); rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("text/plain: status = %d, queria 415", rec.Code)
	}
	if rec := reqJSON(h, http.MethodPost, "/api/push/subscription", strings.Replace(inscricaoValida, "https://", "http://", 1), c, "application/json"); rec.Code != http.StatusBadRequest {
		t.Errorf("endpoint http: status = %d, queria 400", rec.Code)
	}
	if rec := reqJSON(h, http.MethodPost, "/api/push/subscription", `{"endpoint":`+strings.Repeat(" ", MaxPushBytes)+`"x"}`, c, "application/json"); rec.Code != http.StatusBadRequest {
		t.Errorf("corpo grande: status = %d, queria 400", rec.Code)
	}
	if rec := reqJSON(h, http.MethodPost, "/api/push/subscription", inscricaoValida, c, "application/json"); rec.Code != http.StatusNoContent || len(pf.subs) != 1 {
		t.Fatalf("inscrição válida: status = %d, inscritos = %d", rec.Code, len(pf.subs))
	}

	if rec := reqJSON(h, http.MethodPost, "/api/push/test", "", c, "text/plain"); rec.Code != http.StatusUnsupportedMediaType || len(pf.enviada) != 0 {
		t.Errorf("teste com text/plain: status = %d, enviadas = %d; queria 415 e 0", rec.Code, len(pf.enviada))
	}
	rec = reqJSON(h, http.MethodPost, "/api/push/test", "", c, "application/json")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"sent":1`) || len(pf.enviada) != 1 {
		t.Errorf("teste: %d %s", rec.Code, rec.Body)
	}

	if rec := reqJSON(h, http.MethodDelete, "/api/push/subscription", `{"endpoint":"https://fcm.googleapis.com/fcm/send/abc"}`, c, "application/json"); rec.Code != http.StatusNoContent || len(pf.subs) != 0 {
		t.Errorf("desinscrever: status = %d, inscritos = %d", rec.Code, len(pf.subs))
	}
}

// ---------- Uso do plano ----------

// statusline monta um JSON do status line com transcript_path e as duas
// janelas, além de campos que o servidor precisa ignorar.
func statusline(transcriptPath string) string {
	return `{
  "session_id": "sess-1",
  "transcript_path": "` + transcriptPath + `",
  "cwd": "/home/usuario/` + sentinela + `",
  "model": {"id": "claude-opus-4-1", "display_name": "Opus"},
  "cost": {"total_cost_usd": 0.42},
  "rate_limits": {
    "five_hour": {"used_percentage": 23.5, "resets_at": 1792171200},
    "seven_day": {"used_percentage": 41, "resets_at": 1792603200}
  }
}`
}

// usoFalso registra as chamadas de Apply.
type usoFalso struct {
	contas []string
	snap   []ratelimit.Entry
}

func (u *usoFalso) Apply(conta string, r ratelimit.Reading) bool {
	u.contas = append(u.contas, conta)
	return true
}

func (u *usoFalso) Snapshot() []ratelimit.Entry { return u.snap }

func TestStatuslineSoDoLoopback(t *testing.T) {
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
			_, srv, _ := novoServidor(t, "tok")
			u := &usoFalso{}
			srv.SetUsage(u, nil)
			h := srv.Handler()
			rec := do(h, http.MethodPost, "/statusline", tt.remote, statusline("/home/usuario/.claude/projects/p/a.jsonl"))
			if rec.Code != tt.want {
				t.Fatalf("status = %d, queria %d", rec.Code, tt.want)
			}
			if aplicou := len(u.contas) == 1; aplicou != (tt.want == http.StatusNoContent) {
				t.Errorf("Apply chamado %d vezes", len(u.contas))
			}
		})
	}
}

func TestStatuslineResolveConta(t *testing.T) {
	duas, err := account.Parse([]string{"pessoal=/home/usuario/.claude-pessoal", "trabalho=/home/usuario/.claude-trabalho"}, "/home/usuario")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		accounts *account.List
		path     string
		want     []string // contas passadas ao Apply
	}{
		{"conta pessoal", duas, "/home/usuario/.claude-pessoal/projects/p/a.jsonl", []string{"pessoal"}},
		{"conta trabalho", duas, "/home/usuario/.claude-trabalho/projects/p/a.jsonl", []string{"trabalho"}},
		{"fora das contas é descartado", duas, "/home/usuario/.claude/projects/p/a.jsonl", nil},
		{"caminho vazio com contas é descartado", duas, "", nil},
		{"sem contas vai para a conta vazia", nil, "/home/usuario/.claude/projects/p/a.jsonl", []string{""}},
		{"sem contas e sem caminho vai para a conta vazia", nil, "", []string{""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, srv, _ := novoServidor(t, "")
			u := &usoFalso{}
			srv.SetUsage(u, tt.accounts)
			rec := do(srv.Handler(), http.MethodPost, "/statusline", "127.0.0.1:1", statusline(tt.path))
			if rec.Code != http.StatusNoContent {
				t.Fatalf("status = %d, queria 204", rec.Code)
			}
			if strings.Join(u.contas, ",") != strings.Join(tt.want, ",") || len(u.contas) != len(tt.want) {
				t.Errorf("Apply em %q, queria %q", u.contas, tt.want)
			}
		})
	}
}

func TestStatuslineSemUsageResponde204(t *testing.T) {
	tests := []struct {
		name   string
		remote string
		body   string
		want   int
	}{
		{"leitura válida", "127.0.0.1:1", statusline("/x/projects/p/a.jsonl"), http.StatusNoContent},
		{"JSON inválido também é descartado", "127.0.0.1:1", "{", http.StatusNoContent},
		{"fora do loopback continua proibido", "100.64.0.10:1", statusline("/x/projects/p/a.jsonl"), http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, srv, h := novoServidor(t, "")
			if rec := do(h, http.MethodPost, "/statusline", tt.remote, tt.body); rec.Code != tt.want {
				t.Errorf("status = %d, queria %d", rec.Code, tt.want)
			}
			if n := srv.StatuslineErrors(); n != 0 {
				t.Errorf("StatuslineErrors = %d, queria 0", n)
			}
		})
	}
}

func TestStatuslinePayloadInvalido(t *testing.T) {
	grande := `{"transcript_path":"` + strings.Repeat("a", ratelimit.MaxBytes) + `"}`
	tests := []struct {
		name string
		body string
		want int
	}{
		{"JSON inválido", `{` + sentinela, http.StatusBadRequest},
		{"JSON truncado", `{"rate_limits": {"five_hour": "` + sentinela, http.StatusBadRequest},
		{"corpo grande", grande, http.StatusRequestEntityTooLarge},
	}
	_, srv, _ := novoServidor(t, "")
	u := &usoFalso{}
	srv.SetUsage(u, nil)
	h := srv.Handler()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(h, http.MethodPost, "/statusline", "127.0.0.1:1", tt.body)
			if rec.Code != tt.want {
				t.Errorf("status = %d, queria %d", rec.Code, tt.want)
			}
			if b := rec.Body.String(); strings.Contains(b, sentinela) || strings.Contains(b, "aaaa") {
				t.Errorf("resposta cita o corpo: %q", b)
			}
		})
	}
	if n := srv.StatuslineErrors(); n != int64(len(tests)) {
		t.Errorf("StatuslineErrors = %d, queria %d", n, len(tests))
	}
	if len(u.contas) != 0 {
		t.Errorf("Apply chamado com payload inválido: %q", u.contas)
	}
	if rec := do(h, http.MethodGet, "/statusline", "127.0.0.1:1", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /statusline: status = %d, queria 405", rec.Code)
	}
}

func TestApiUsoExigeToken(t *testing.T) {
	tests := []struct {
		name   string
		target string
		cookie *http.Cookie
		want   int
	}{
		{"sem credencial", "/api/uso", nil, http.StatusUnauthorized},
		{"token errado", "/api/uso?t=errado", nil, http.StatusUnauthorized},
		{"cookie errado", "/api/uso", &http.Cookie{Name: CookieName, Value: "errado"}, http.StatusUnauthorized},
		{"token na query", "/api/uso?t=tok", nil, http.StatusOK},
		{"cookie", "/api/uso", &http.Cookie{Name: CookieName, Value: "tok"}, http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, h := novoServidor(t, "tok")
			var cs []*http.Cookie
			if tt.cookie != nil {
				cs = append(cs, tt.cookie)
			}
			rec := do(h, http.MethodGet, tt.target, "100.64.0.10:1", "", cs...)
			if rec.Code != tt.want {
				t.Errorf("status = %d, queria %d", rec.Code, tt.want)
			}
		})
	}
}

func TestApiUsoListaVaziaNuncaNull(t *testing.T) {
	tests := []struct {
		name  string
		usage Usage
	}{
		{"sem SetUsage", nil},
		{"Snapshot nil", &usoFalso{}},
		{"Snapshot vazio", &usoFalso{snap: []ratelimit.Entry{}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, srv, _ := novoServidor(t, "")
			if tt.usage != nil {
				srv.SetUsage(tt.usage, nil)
			}
			rec := do(srv.Handler(), http.MethodGet, "/api/uso", "127.0.0.1:1", "")
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, queria 200", rec.Code)
			}
			if got := strings.TrimSpace(rec.Body.String()); got != "[]" {
				t.Errorf("corpo = %q, queria []", got)
			}
		})
	}
}

// TestApiUsoSoCamposDeEntry passa o status line completo pelo Tracker real e
// confere que /api/uso devolve só os campos de ratelimit.Entry.
func TestApiUsoSoCamposDeEntry(t *testing.T) {
	fixture, err := os.ReadFile("../ratelimit/testdata/statusline-completo.json")
	if err != nil {
		t.Fatal(err)
	}
	contas, err := account.Parse([]string{"pessoal=/home/usuario/.claude"}, "/home/usuario")
	if err != nil {
		t.Fatal(err)
	}
	tr, err := ratelimit.Load("", []string{"pessoal"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, srv, _ := novoServidor(t, "")
	srv.SetUsage(tr, contas)
	h := srv.Handler()
	if rec := do(h, http.MethodPost, "/statusline", "127.0.0.1:1", string(fixture)); rec.Code != http.StatusNoContent {
		t.Fatalf("POST /statusline: status = %d", rec.Code)
	}

	rec := do(h, http.MethodGet, "/api/uso", "127.0.0.1:1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/uso: status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, proibido := range []string{"transcript_path", "/home/usuario", "session_id", "cost", "context_window", "spend_limit", "model", "abc.jsonl"} {
		if strings.Contains(body, proibido) {
			t.Errorf("resposta contém %q: %s", proibido, body)
		}
	}
	var entries []map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &entries); err != nil {
		t.Fatalf("JSON inválido: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entradas = %d, queria 1: %s", len(entries), body)
	}
	permitidos := map[string]bool{"account": true, "five_hour": true, "seven_day": true, "received_at": true}
	for k := range entries[0] {
		if !permitidos[k] {
			t.Errorf("campo inesperado %q", k)
		}
	}
	for k := range permitidos {
		if _, ok := entries[0][k]; !ok {
			t.Errorf("faltou o campo %q", k)
		}
	}
	if string(entries[0]["account"]) != `"pessoal"` {
		t.Errorf("account = %s, queria \"pessoal\"", entries[0]["account"])
	}
}
