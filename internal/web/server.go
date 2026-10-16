package web

import (
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
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

//go:embed static/index.html static/manifest.webmanifest static/*.png
var static embed.FS

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
// primeira visita, que grava um cookie HttpOnly; depois, só o cookie.
func (s *Server) auth(next http.Handler) http.Handler {
	if s.token == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if t := r.URL.Query().Get("t"); t != "" && s.tokenOK(t) {
			http.SetCookie(w, &http.Cookie{
				Name:     CookieName,
				Value:    s.token,
				Path:     "/",
				HttpOnly: true,
				Secure:   r.TLS != nil, // em HTTPS o cookie nunca volta por HTTP
				SameSite: http.SameSiteStrictMode,
				MaxAge:   int((30 * 24 * time.Hour).Seconds()),
			})
			next.ServeHTTP(w, r)
			return
		}
		if c, err := r.Cookie(CookieName); err == nil && s.tokenOK(c.Value) {
			next.ServeHTTP(w, r)
			return
		}
		http.Error(w, "não autorizado", http.StatusUnauthorized)
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
