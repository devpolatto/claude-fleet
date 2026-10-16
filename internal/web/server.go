package web

import (
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"html/template"
	"log"
	"net"
	"net/http"
	"net/netip"
	"sync/atomic"
	"time"

	"github.com/devpolatto/claude-fleet/internal/ingest"
	"github.com/devpolatto/claude-fleet/internal/store"
)

// MaxEventBytes é o tamanho máximo do corpo de POST /event.
const MaxEventBytes = 256 << 10

// CookieName é o cookie que guarda o token depois da primeira visita com ?t=.
const CookieName = "fleet_token"

// MaxLoginBytes limita o corpo de POST /login (só o campo token).
const MaxLoginBytes = 4 << 10

// cookieMaxAge é a validade do cookie, renovada a cada visita à página.
const cookieMaxAge = 30 * 24 * time.Hour

//go:embed static/index.html static/login.html static/manifest.webmanifest static/*.png
var static embed.FS

// loginPage é a tela de token, servida no lugar do 401 em texto quando quem
// pede é a página: no app instalado não há barra de endereço para o ?t=.
var loginPage = template.Must(template.ParseFS(static, "static/login.html"))

// publicAssets são os arquivos que o navegador busca sem cookie (o manifesto
// é pedido com credentials "omit") e que não dizem nada sobre a máquina: ficam
// fora da autenticação por token.
var publicAssets = map[string]string{
	"/manifest.webmanifest": "application/manifest+json",
	"/icon-192.png":         "image/png",
	"/icon-512.png":         "image/png",
	"/apple-touch-icon.png": "image/png",
}

// Store é o que o servidor precisa do store.Store.
type Store interface {
	Source
	Apply(ev ingest.Event, now time.Time) bool
	Get(id string) (store.SessionDetail, bool)
}

// Server monta as rotas do painel.
type Server struct {
	store Store
	hub   http.Handler
	token string
	now   func() time.Time

	// eventErrors conta payloads recusados em /event (400/413), sem guardar
	// nada do corpo.
	eventErrors atomic.Int64
}

// NewServer cria o servidor. Com token vazio, as rotas de leitura ficam
// abertas; isso só é aceito com bind em loopback (validado no main).
func NewServer(st Store, hub http.Handler, token string) *Server {
	return &Server{store: st, hub: hub, token: token, now: time.Now}
}

// EventErrors devolve quantos payloads foram recusados em /event.
func (s *Server) EventErrors() int64 { return s.eventErrors.Load() }

// Handler devolve o roteador completo.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	// /event fica fora da autenticação por token: o hook não conhece o
	// token. A proteção é aceitar só conexões de loopback.
	mux.HandleFunc("POST /event", s.handleEvent)
	mux.HandleFunc("POST /login", s.handleLogin)
	for path, ctype := range publicAssets {
		mux.Handle("GET "+path, assetHandler("static"+path, ctype))
	}

	mux.Handle("GET /{$}", s.auth(http.HandlerFunc(s.handleIndex)))
	mux.Handle("GET /stream", s.auth(s.hub))
	mux.Handle("GET /api/sessions", s.auth(http.HandlerFunc(s.handleSessions)))
	mux.Handle("GET /api/session/{id}", s.auth(http.HandlerFunc(s.handleSession)))
	return mux
}

func (s *Server) handleEvent(w http.ResponseWriter, r *http.Request) {
	if !remoteIsLoopback(r.RemoteAddr) {
		http.Error(w, "proibido", http.StatusForbidden)
		return
	}
	ev, err := ingest.Parse(r.Body, MaxEventBytes)
	if err != nil {
		s.eventErrors.Add(1)
		status := http.StatusBadRequest
		if errors.Is(err, ingest.ErrTooLarge) {
			status = http.StatusRequestEntityTooLarge
		}
		// A mensagem dos erros do ingest nunca cita o corpo.
		http.Error(w, http.StatusText(status), status)
		return
	}
	s.store.Apply(ev, s.now())
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	page, err := static.ReadFile("static/index.html")
	if err != nil {
		http.Error(w, "página indisponível", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(page)
}

// assetHandler serve um arquivo embutido com o Content-Type dado.
func assetHandler(name, ctype string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := static.ReadFile(name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", ctype)
		w.Header().Set("Cache-Control", "public, max-age=86400")
		_, _ = w.Write(data)
	})
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.store.Snapshot())
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	d, ok := s.store.Get(r.PathValue("id"))
	if !ok {
		http.Error(w, "sessão não encontrada", http.StatusNotFound)
		return
	}
	writeJSON(w, d)
}

// auth exige o token quando ele está configurado. O token chega por ?t= na
// primeira visita ou pela tela de login, que gravam um cookie HttpOnly;
// depois, só o cookie.
//
// Na página (GET /), um ?t= válido grava o cookie e redireciona para a URL
// sem o token, que assim não fica na barra nem no histórico; o cookie é
// renovado a cada visita, e sem credencial a resposta é a tela de login. As
// demais rotas aceitam ?t= sem redirecionar e respondem 401 em texto.
func (s *Server) auth(next http.Handler) http.Handler {
	if s.token == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Path == "/"
		if t := r.URL.Query().Get("t"); t != "" {
			if s.tokenOK(t) {
				s.setCookie(w, r)
				if page {
					q := r.URL.Query()
					q.Del("t")
					u := *r.URL
					u.RawQuery = q.Encode()
					http.Redirect(w, r, u.RequestURI(), http.StatusSeeOther)
					return
				}
				next.ServeHTTP(w, r)
				return
			}
			if page {
				s.loginPage(w, http.StatusUnauthorized, true)
				return
			}
		}
		if c, err := r.Cookie(CookieName); err == nil && s.tokenOK(c.Value) {
			if page {
				s.setCookie(w, r) // validade conta a partir do último uso
			}
			next.ServeHTTP(w, r)
			return
		}
		if page {
			s.loginPage(w, http.StatusUnauthorized, false)
			return
		}
		http.Error(w, "não autorizado", http.StatusUnauthorized)
	})
}

// handleLogin recebe o token da tela de login. Válido: grava o cookie e volta
// para a página; inválido: mostra a tela de novo com o aviso.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if s.token == "" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxLoginBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	if !s.tokenOK(r.PostForm.Get("token")) {
		s.loginPage(w, http.StatusUnauthorized, true)
		return
	}
	s.setCookie(w, r)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) loginPage(w http.ResponseWriter, status int, erro bool) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := loginPage.Execute(w, struct{ Erro bool }{erro}); err != nil {
		log.Printf("web: falha ao montar a tela de login: %v", err)
	}
}

// setCookie grava o cookie do token. SameSite=Lax, e não Strict: abrir o app
// instalado pelo ícone chega como navegação vinda de fora, e o navegador não
// envia cookie Strict nesse caso. Lax não abre brecha aqui: as rotas com
// cookie só leem, e os POSTs (/event, /login) não dependem dele.
func (s *Server) setCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    s.token,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil, // em HTTPS o cookie nunca volta por HTTP
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(cookieMaxAge.Seconds()),
	})
}

func (s *Server) tokenOK(got string) bool {
	return subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) == 1
}

func remoteIsLoopback(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return false
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	return ip.Unmap().IsLoopback()
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("web: falha ao serializar resposta: %v", err)
	}
}
