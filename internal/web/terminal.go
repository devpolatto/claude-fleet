package web

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"sync"
	"time"

	"github.com/devpolatto/claude-fleet/internal/account"
	"github.com/devpolatto/claude-fleet/internal/terminal"
)

// Terminal é o que o servidor precisa do terminal.Terminal (AD-010).
type Terminal interface {
	Locate(ctx context.Context, configDir, sessionID string) terminal.Location
	Open(ctx context.Context, loc terminal.Location) error
}

// openInterval é o intervalo mínimo entre duas janelas da mesma sessão.
const openInterval = 3 * time.Second

// maxOpenBytes limita o corpo de POST .../terminal/abrir ("{}").
const maxOpenBytes = 1 << 10

// terminalState guarda o que as rotas do terminal usam; zero quando
// SetTerminal não foi chamado.
type terminalState struct {
	term       Terminal
	focusFrom  []netip.Addr
	localAddrs func() []netip.Addr

	mu       sync.Mutex
	lastOpen map[string]time.Time // sessão → último pedido aceito
}

// SetTerminal liga as rotas do terminal. allowed são os IPs, além da própria
// máquina, que podem pedir para abrir o terminal (-focus-from). Chamar antes
// de Handler; sem isso as rotas respondem 404.
func (s *Server) SetTerminal(allowed []netip.Addr, t Terminal) {
	s.terminal.term = t
	s.terminal.focusFrom = make([]netip.Addr, len(allowed))
	for i, a := range allowed {
		s.terminal.focusFrom[i] = a.Unmap()
	}
	if s.terminal.localAddrs == nil {
		s.terminal.localAddrs = interfaceAddrs
	}
	s.terminal.lastOpen = map[string]time.Time{}
}

// interfaceAddrs lista os IPs das interfaces da máquina. O serviço escuta no
// IP do Tailscale, então o navegador da própria máquina chega com esse IP de
// origem, e não com o do loopback.
func interfaceAddrs() []netip.Addr {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	out := make([]netip.Addr, 0, len(addrs))
	for _, a := range addrs {
		if p, err := netip.ParsePrefix(a.String()); err == nil {
			out = append(out, p.Addr().Unmap())
		}
	}
	return out
}

// trustedPeer diz se o pedido vem da própria máquina ou de um IP de
// -focus-from. Comparação exata de endereço, sem faixa.
func (s *Server) trustedPeer(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	ip = ip.Unmap().WithZone("")
	if ip.IsLoopback() {
		return true
	}
	for _, a := range s.terminal.focusFrom {
		if a == ip {
			return true
		}
	}
	if s.terminal.localAddrs != nil {
		for _, a := range s.terminal.localAddrs() {
			if a.WithZone("") == ip {
				return true
			}
		}
	}
	return false
}

// focusAllowed é a regra da rota que abre o terminal: aparelho autorizado,
// corpo JSON e pedido feito pela própria página do painel (Sec-Fetch-Site
// same-origin e Origin com o mesmo host). Uma página de outro site, mesmo
// aberta no navegador do dono, não passa. Escreve o erro e devolve false
// quando recusa.
func (s *Server) focusAllowed(w http.ResponseWriter, r *http.Request) bool {
	if !s.trustedPeer(r) {
		http.Error(w, "aparelho sem permissão para abrir o terminal", http.StatusForbidden)
		return false
	}
	if r.Header.Get("Content-Type") != "application/json" {
		http.Error(w, "Content-Type deve ser application/json", http.StatusUnsupportedMediaType)
		return false
	}
	if r.Header.Get("Sec-Fetch-Site") != "same-origin" {
		http.Error(w, "proibido", http.StatusForbidden)
		return false
	}
	o, err := url.Parse(r.Header.Get("Origin"))
	if err != nil || o.Host == "" || o.Host != r.Host {
		http.Error(w, "proibido", http.StatusForbidden)
		return false
	}
	return true
}

// locate acha a sessão no store e onde ela roda. ok=false quando a sessão
// não existe (a resposta 404 já foi escrita).
func (s *Server) locate(w http.ResponseWriter, r *http.Request) (terminal.Location, string, bool) {
	if s.terminal.term == nil {
		http.NotFound(w, r)
		return terminal.Location{}, "", false
	}
	id := r.PathValue("id")
	d, ok := s.store.Get(id)
	if !ok {
		http.Error(w, "sessão não encontrada", http.StatusNotFound)
		return terminal.Location{}, "", false
	}
	return s.terminal.term.Locate(r.Context(), account.ConfigDir(d.TranscriptPath), id), id, true
}

// terminalView é a resposta de GET /api/session/{id}/terminal: só o
// metadado de localização (AD-003), sem PID, pane nem caminho.
type terminalView struct {
	Estado    terminal.Estado `json:"estado"`
	Rotulo    string          `json:"rotulo"`
	PodeAbrir bool            `json:"podeAbrir"`
}

func (s *Server) handleTerminal(w http.ResponseWriter, r *http.Request) {
	loc, _, ok := s.locate(w, r)
	if !ok {
		return
	}
	writeJSON(w, terminalView{
		Estado:    loc.Estado,
		Rotulo:    loc.Rotulo,
		PodeAbrir: loc.Estado == terminal.EstadoTmux && s.trustedPeer(r),
	})
}

// handleTerminalOpen abre uma janela nova do terminal presa ao pane da
// sessão. A localização é refeita aqui, sem confiar no que o pop-up viu.
func (s *Server) handleTerminalOpen(w http.ResponseWriter, r *http.Request) {
	if s.terminal.term == nil {
		http.NotFound(w, r)
		return
	}
	if !s.focusAllowed(w, r) {
		return
	}
	var body struct{}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxOpenBytes)).Decode(&body); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	loc, id, ok := s.locate(w, r)
	if !ok {
		return
	}
	if loc.Estado != terminal.EstadoTmux {
		http.Error(w, "sessão encerrada ou fora do tmux", http.StatusConflict)
		return
	}
	if !s.reserveOpen(id) {
		w.Header().Set("Retry-After", "3")
		http.Error(w, "terminal aberto há pouco", http.StatusTooManyRequests)
		return
	}
	if err := s.terminal.term.Open(r.Context(), loc); err != nil {
		log.Printf("web: abrir terminal em %s: %v", loc.Rotulo, err)
		http.Error(w, "não consegui abrir o terminal", http.StatusBadGateway)
		return
	}
	log.Printf("web: terminal aberto em %s", loc.Rotulo)
	w.WriteHeader(http.StatusNoContent)
}

// reserveOpen aceita no máximo um pedido por sessão a cada openInterval e
// descarta as marcas vencidas.
func (s *Server) reserveOpen(id string) bool {
	now := s.now()
	t := &s.terminal
	t.mu.Lock()
	defer t.mu.Unlock()
	for k, at := range t.lastOpen {
		if now.Sub(at) >= openInterval {
			delete(t.lastOpen, k)
		}
	}
	if _, busy := t.lastOpen[id]; busy {
		return false
	}
	t.lastOpen[id] = now
	return true
}
