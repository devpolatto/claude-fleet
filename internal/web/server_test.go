package web

import (
	"bufio"
	"context"
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
	t.Run("?t= grava cookie", func(t *testing.T) {
		rec := do(h, http.MethodGet, "/?t="+token, "100.64.0.10:1", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, queria 200", rec.Code)
		}
		cookies := rec.Result().Cookies()
		if len(cookies) != 1 {
			t.Fatalf("cookies = %d, queria 1", len(cookies))
		}
		cookie = cookies[0]
		if cookie.Name != CookieName || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" {
			t.Errorf("cookie = %+v, queria HttpOnly, SameSite=Strict e Path=/", cookie)
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
