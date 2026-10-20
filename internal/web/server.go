package web

import (
	"context"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"html/template"
	"log"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/devpolatto/claude-fleet/internal/account"
	"github.com/devpolatto/claude-fleet/internal/ingest"
	"github.com/devpolatto/claude-fleet/internal/push"
	"github.com/devpolatto/claude-fleet/internal/ratelimit"
	"github.com/devpolatto/claude-fleet/internal/store"
)

// MaxEventBytes é o tamanho máximo do corpo de POST /event.
const MaxEventBytes = 256 << 10

// CookieName é o cookie que guarda o token depois da primeira visita com ?t=.
const CookieName = "fleet_token"

// MaxPushBytes limita o corpo das rotas de push (uma inscrição tem ~400 bytes).
const MaxPushBytes = 4 << 10

// MaxLoginBytes limita o corpo de POST /login (só o campo token).
const MaxLoginBytes = 4 << 10

// Prazos do http.Server. Os corpos aceitos são pequenos (no máximo
// MaxEventBytes), então 30 s para ler ou escrever uma requisição inteira
// sobram; o /stream renova os dele (sseWriteWait). Conexão ociosa fecha em 2
// min, para um cliente lento não segurar descritores.
const (
	readHeaderTimeout = 10 * time.Second
	readTimeout       = 30 * time.Second
	writeTimeout      = 30 * time.Second
	idleTimeout       = 2 * time.Minute
)

// NewHTTPServer devolve o http.Server do painel com os prazos acima.
func NewHTTPServer(h http.Handler) *http.Server {
	return &http.Server{
		Handler:           h,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}
}

// cookieMaxAge é a validade do cookie, renovada a cada visita à página.
const cookieMaxAge = 30 * 24 * time.Hour

//go:embed static/index.html static/login.html static/manifest.webmanifest static/sw.js static/*.png
var static embed.FS

// loginPage é a tela de token, servida no lugar do 401 em texto quando quem
// pede é a página: no app instalado não há barra de endereço para o ?t=.
var loginPage = template.Must(template.ParseFS(static, "static/login.html"))

// asset é um arquivo embutido servido fora da autenticação.
type asset struct {
	ctype, cache string
}

// publicAssets são os arquivos que o navegador busca sem cookie (o manifesto
// é pedido com credentials "omit") e que não dizem nada sobre a máquina: ficam
// fora da autenticação por token. O sw.js vai com no-cache para o navegador
// pegar uma versão nova logo após a atualização do binário.
var publicAssets = map[string]asset{
	"/manifest.webmanifest": {"application/manifest+json", "public, max-age=86400"},
	"/icon-192.png":         {"image/png", "public, max-age=86400"},
	"/icon-512.png":         {"image/png", "public, max-age=86400"},
	"/apple-touch-icon.png": {"image/png", "public, max-age=86400"},
	"/sw.js":                {"text/javascript; charset=utf-8", "no-cache"},
}

// Store é o que o servidor precisa do store.Store.
type Store interface {
	Source
	Apply(ev ingest.Event, now time.Time) bool
	Get(id string) (store.SessionDetail, bool)
}

// Push é o que o servidor precisa do push.Service.
type Push interface {
	PublicKey() string
	Subscribe(sub push.Subscription) error
	Unsubscribe(endpoint string) error
	SendAll(ctx context.Context, msg push.Message, topic string) int
}

// Usage é o que o servidor precisa do ratelimit.Tracker.
type Usage interface {
	Apply(account string, r ratelimit.Reading) bool
	Snapshot() []ratelimit.Entry
}

// Server monta as rotas do painel.
type Server struct {
	store    Store
	hub      http.Handler
	push     Push          // nil: push desligado, as rotas respondem 503
	usage    Usage         // nil: /statusline descarta e /api/uso devolve []
	accounts *account.List // nil: sem -account, tudo na conta ""
	token    string
	now      func() time.Time

	// eventErrors conta payloads recusados em /event (400/413), sem guardar
	// nada do corpo.
	eventErrors atomic.Int64
	// statuslineErrors faz o mesmo para /statusline.
	statuslineErrors atomic.Int64
	// failures limita as tentativas de token errado por IP.
	failures failures
}

// NewServer cria o servidor. Com token vazio, as rotas de leitura ficam
// abertas; isso só é aceito com bind em loopback (validado no main).
func NewServer(st Store, hub http.Handler, token string) *Server {
	return &Server{store: st, hub: hub, token: token, now: time.Now}
}

// SetPush liga as rotas de push; chamar antes de Handler.
func (s *Server) SetPush(p Push) { s.push = p }

// SetUsage liga o uso do plano: /statusline passa a registrar as leituras
// em u, na conta resolvida por accounts (nil: conta ""); chamar antes de
// Handler.
func (s *Server) SetUsage(u Usage, accounts *account.List) {
	s.usage = u
	s.accounts = accounts
}

// EventErrors devolve quantos payloads foram recusados em /event.
func (s *Server) EventErrors() int64 { return s.eventErrors.Load() }

// StatuslineErrors devolve quantos payloads foram recusados em /statusline.
func (s *Server) StatuslineErrors() int64 { return s.statuslineErrors.Load() }

// Handler devolve o roteador completo.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	// /event fica fora da autenticação por token: o hook não conhece o
	// token. A proteção é aceitar só conexões de loopback.
	mux.HandleFunc("POST /event", s.handleEvent)
	// /statusline segue a mesma regra: o script do status line não conhece
	// o token.
	mux.HandleFunc("POST /statusline", s.handleStatusline)
	mux.HandleFunc("POST /login", s.handleLogin)
	for path, a := range publicAssets {
		mux.Handle("GET "+path, assetHandler("static"+path, a))
	}

	mux.Handle("GET /{$}", s.auth(http.HandlerFunc(s.handleIndex)))
	mux.Handle("GET /stream", s.auth(s.hub))
	mux.Handle("GET /api/sessions", s.auth(http.HandlerFunc(s.handleSessions)))
	mux.Handle("GET /api/session/{id}", s.auth(http.HandlerFunc(s.handleSession)))
	mux.Handle("GET /api/uso", s.auth(http.HandlerFunc(s.handleUso)))
	mux.Handle("GET /api/push/key", s.auth(http.HandlerFunc(s.handlePushKey)))
	mux.Handle("POST /api/push/subscription", s.auth(http.HandlerFunc(s.handlePushSubscribe)))
	mux.Handle("DELETE /api/push/subscription", s.auth(http.HandlerFunc(s.handlePushUnsubscribe)))
	mux.Handle("POST /api/push/test", s.auth(http.HandlerFunc(s.handlePushTest)))
	if s.token == "" {
		return loopbackHost(mux)
	}
	return mux
}

// loopbackHost recusa, no modo sem token, requisições cujo Host não seja um
// nome de loopback. Sem isso, uma página com DNS rebinding para 127.0.0.1
// fica same-origin com o painel e lê as rotas abertas. Com token, o cookie
// já amarra o acesso à origem certa.
func loopbackHost(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		if !isLoopbackName(host) {
			http.Error(w, http.StatusText(http.StatusMisdirectedRequest), http.StatusMisdirectedRequest)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// isLoopbackName aceita "localhost" e IPs de loopback, com ou sem colchetes.
func isLoopbackName(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip, err := netip.ParseAddr(strings.TrimSuffix(strings.TrimPrefix(host, "["), "]"))
	return err == nil && ip.Unmap().IsLoopback()
}

func (s *Server) handleEvent(w http.ResponseWriter, r *http.Request) {
	if !fromLocalTool(w, r) {
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

// handleStatusline recebe o JSON do status line do Claude Code e guarda só os
// percentuais do plano. Nem o corpo nem o transcript_path vão para log ou
// resposta.
func (s *Server) handleStatusline(w http.ResponseWriter, r *http.Request) {
	if !fromLocalTool(w, r) {
		return
	}
	if s.usage == nil {
		// Uso do plano desligado: o script nunca vê erro.
		w.WriteHeader(http.StatusNoContent)
		return
	}
	rd, err := ratelimit.Parse(r.Body, ratelimit.MaxBytes)
	if err != nil {
		s.statuslineErrors.Add(1)
		status := http.StatusBadRequest
		if errors.Is(err, ratelimit.ErrTooLarge) {
			status = http.StatusRequestEntityTooLarge
		}
		http.Error(w, http.StatusText(status), status)
		return
	}
	acc := ""
	if s.accounts != nil {
		if rd.TranscriptPath == "" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		acc = s.accounts.Resolve(rd.TranscriptPath)
		if acc == account.Outra {
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}
	s.usage.Apply(acc, rd)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleUso(w http.ResponseWriter, r *http.Request) {
	entries := []ratelimit.Entry{}
	if s.usage != nil {
		if snap := s.usage.Snapshot(); snap != nil {
			entries = snap
		}
	}
	writeJSON(w, entries)
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

// assetHandler serve um arquivo embutido com o Content-Type e o
// Cache-Control do asset.
func assetHandler(name string, a asset) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := static.ReadFile(name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", a.ctype)
		w.Header().Set("Cache-Control", a.cache)
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
		if s.throttled(w, r) {
			return
		}
		page := r.URL.Path == "/"
		if t := r.URL.Query().Get("t"); t != "" {
			if s.tryToken(r, t) {
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
		if c, err := r.Cookie(CookieName); err == nil && s.tryToken(r, c.Value) {
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
	if s.throttled(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxLoginBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	if !s.tryToken(r, r.PostForm.Get("token")) {
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
// envia cookie Strict nesse caso. Lax não abre brecha aqui: em POST e DELETE
// vindos de outro site o navegador não manda cookie Lax, e as rotas de push
// ainda exigem corpo JSON, que um formulário de outro site não envia.
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

// tryToken compara got com o token e, se errado, conta a falha para o IP de
// origem (?t=, cookie ou tela de login).
func (s *Server) tryToken(r *http.Request, got string) bool {
	if s.tokenOK(got) {
		return true
	}
	s.failures.add(ipOf(r.RemoteAddr), s.now())
	return false
}

// throttled responde 429 quando o IP de origem esgotou as tentativas de token;
// nem um token certo é testado até a janela acabar.
func (s *Server) throttled(w http.ResponseWriter, r *http.Request) bool {
	if !s.failures.blocked(ipOf(r.RemoteAddr), s.now()) {
		return false
	}
	w.Header().Set("Retry-After", strconv.Itoa(int(failureWindow.Seconds())))
	http.Error(w, "muitas tentativas; tente de novo em um minuto", http.StatusTooManyRequests)
	return true
}

// fromLocalTool aceita só o que vem do hook ou do script do status line: peer
// de loopback, Content-Type application/json e nenhum sinal de navegador. O
// navegador do dono também é peer de loopback e repassa POSTs "simples"
// (text/plain, sem preflight) de qualquer site; Origin e Sec-Fetch-Site
// denunciam esse caminho, e o curl dos scripts não manda nenhum dos dois.
// Escreve a resposta de erro e devolve false quando recusa.
func fromLocalTool(w http.ResponseWriter, r *http.Request) bool {
	if !remoteIsLoopback(r.RemoteAddr) || r.Header.Get("Origin") != "" {
		http.Error(w, "proibido", http.StatusForbidden)
		return false
	}
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "none" {
		http.Error(w, "proibido", http.StatusForbidden)
		return false
	}
	if r.Header.Get("Content-Type") != "application/json" {
		http.Error(w, "Content-Type deve ser application/json", http.StatusUnsupportedMediaType)
		return false
	}
	return true
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

// ---------- Push ----------

func (s *Server) pushOff(w http.ResponseWriter) bool {
	if s.push == nil {
		http.Error(w, "push desligado", http.StatusServiceUnavailable)
		return true
	}
	return false
}

// readPushJSON decodifica o corpo JSON das rotas de push, recusando outro
// Content-Type e corpos grandes.
func readPushJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if ct := r.Header.Get("Content-Type"); ct != "application/json" {
		http.Error(w, "Content-Type deve ser application/json", http.StatusUnsupportedMediaType)
		return false
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxPushBytes)).Decode(v); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return false
	}
	return true
}

func (s *Server) handlePushKey(w http.ResponseWriter, r *http.Request) {
	if s.pushOff(w) {
		return
	}
	writeJSON(w, map[string]string{"publicKey": s.push.PublicKey()})
}

func (s *Server) handlePushSubscribe(w http.ResponseWriter, r *http.Request) {
	if s.pushOff(w) {
		return
	}
	var sub push.Subscription
	if !readPushJSON(w, r, &sub) {
		return
	}
	if err := sub.Validate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.push.Subscribe(sub); err != nil {
		log.Printf("web: gravar inscrição de push: %v", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePushUnsubscribe(w http.ResponseWriter, r *http.Request) {
	if s.pushOff(w) {
		return
	}
	var body struct {
		Endpoint string `json:"endpoint"`
	}
	if !readPushJSON(w, r, &body) {
		return
	}
	if err := s.push.Unsubscribe(body.Endpoint); err != nil {
		log.Printf("web: apagar inscrição de push: %v", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handlePushTest manda uma notificação de teste a todos os aparelhos.
// handlePushTest exige Content-Type JSON, como as demais rotas de push, mesmo
// sem corpo: sem token não há cookie para barrar o POST "simples" que uma
// página de outro site faz pelo navegador do dono.
func (s *Server) handlePushTest(w http.ResponseWriter, r *http.Request) {
	if s.pushOff(w) {
		return
	}
	if ct := r.Header.Get("Content-Type"); ct != "application/json" {
		http.Error(w, "Content-Type deve ser application/json", http.StatusUnsupportedMediaType)
		return
	}
	n := s.push.SendAll(r.Context(), push.Message{Title: "Claude Fleet", Body: "Notificação de teste", Tag: "fleet-teste", URL: "/"}, "")
	writeJSON(w, map[string]int{"sent": n})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("web: falha ao serializar resposta: %v", err)
	}
}
