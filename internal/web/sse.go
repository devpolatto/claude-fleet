// Package web reúne o servidor HTTP do claude-fleet: página, API e stream SSE.
package web

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/devpolatto/claude-fleet/internal/ratelimit"
	"github.com/devpolatto/claude-fleet/internal/store"
)

// Intervalos padrão do Hub.
const (
	DefaultCoalesce  = 250 * time.Millisecond
	DefaultHeartbeat = 20 * time.Second
)

// sseWriteWait é o prazo de cada escrita no stream. O ReadTimeout e o
// WriteTimeout do http.Server (NewHTTPServer) valem para a conexão inteira e
// derrubariam o SSE; o handler tira o prazo de leitura e renova o de escrita
// a cada envio, de modo que só um cliente que parou de ler cai.
const sseWriteWait = 30 * time.Second

// Source é o que o Hub precisa do Store: um canal de sinais de mudança
// (coalescidos, como em store.Store.Subscribe) e o snapshot atual.
type Source interface {
	Subscribe() <-chan struct{}
	Snapshot() []store.SessionView
}

// UsageSource é o que o Hub precisa do uso do plano (ratelimit.Tracker): um
// canal de sinais de mudança, com o mesmo contrato do Source, e o último uso
// de cada conta.
type UsageSource interface {
	Subscribe() <-chan struct{}
	Snapshot() []ratelimit.Entry
}

// HubOption ajusta um Hub em NewHub.
type HubOption func(*Hub)

// WithUsage faz o Hub mandar também o evento "uso": ao conectar, logo depois
// do snapshot, e a cada mudança coalescida de u. Mudança de uso não reenvia o
// snapshot, e mudança de sessão não reenvia o uso.
func WithUsage(u UsageSource) HubOption {
	return func(h *Hub) { h.usage = u }
}

// Hub atende GET /stream com Server-Sent Events. Uma única goroutine (Run)
// assina a Source, agrupa as mudanças numa janela de Coalesce e envia o
// snapshot a todos os clientes. Cada cliente tem um canal com buffer 1 e o
// envio não bloqueia: um cliente lento perde snapshots intermediários (fica
// sempre com o mais recente) e nunca trava os demais.
type Hub struct {
	// Coalesce é a janela que agrupa mudanças seguidas num único envio.
	Coalesce time.Duration
	// Heartbeat é o intervalo do comentário ": ping" que mantém a conexão viva.
	Heartbeat time.Duration

	src   Source
	usage UsageSource   // nil: sem evento "uso"
	done  chan struct{} // fechado quando Run termina

	mu      sync.Mutex
	clients map[*sseClient]struct{}
}

// sseClient é uma conexão ao /stream. Cada tipo de evento tem o seu canal com
// buffer 1, para um "uso" nunca tomar o lugar de um "snapshot" ainda não lido
// (nem o contrário).
type sseClient struct {
	snapshot chan []byte
	uso      chan []byte
}

// NewHub cria um Hub com os intervalos padrão. Os campos Coalesce e
// Heartbeat podem ser ajustados antes de Run e de servir requisições.
func NewHub(src Source, opts ...HubOption) *Hub {
	h := &Hub{
		Coalesce:  DefaultCoalesce,
		Heartbeat: DefaultHeartbeat,
		src:       src,
		done:      make(chan struct{}),
		clients:   make(map[*sseClient]struct{}),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// pendingSend é um envio agendado na janela de Coalesce. O canal C fica nil
// enquanto não há envio pendente, o que desliga o case no select.
type pendingSend struct {
	timer *time.Timer
	C     <-chan time.Time
}

// schedule agenda o envio, se ainda não houver um pendente: o que já está
// agendado levará o estado mais novo.
func (p *pendingSend) schedule(d time.Duration) {
	if p.C != nil {
		return
	}
	if p.timer == nil {
		p.timer = time.NewTimer(d)
	} else {
		p.timer.Reset(d)
	}
	p.C = p.timer.C
}

func (p *pendingSend) fired() { p.C = nil }

func (p *pendingSend) stop() {
	if p.timer != nil {
		p.timer.Stop()
	}
}

// Run assina a Source (e o UsageSource, se houver) e distribui snapshots e
// uso, cada um com a sua janela de Coalesce, até ctx ser cancelado. Ao sair,
// encerra todas as conexões abertas e passa a recusar novas com 503. Deve
// ser chamado uma única vez.
func (h *Hub) Run(ctx context.Context) {
	defer close(h.done)

	changes := h.src.Subscribe()
	var usageChanges <-chan struct{} // nil sem WithUsage: o case nunca dispara
	if h.usage != nil {
		usageChanges = h.usage.Subscribe()
	}
	var snap, uso pendingSend
	defer snap.stop()
	defer uso.stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-changes:
			snap.schedule(h.Coalesce)
		case <-usageChanges:
			uso.schedule(h.Coalesce)
		case <-snap.C:
			snap.fired()
			msg, err := h.snapshotEvent()
			if err != nil {
				log.Printf("sse: falha ao serializar snapshot: %v", err)
				continue
			}
			h.broadcast(msg, func(c *sseClient) chan []byte { return c.snapshot })
		case <-uso.C:
			uso.fired()
			msg, err := h.usoEvent()
			if err != nil {
				log.Printf("sse: falha ao serializar uso: %v", err)
				continue
			}
			h.broadcast(msg, func(c *sseClient) chan []byte { return c.uso })
		}
	}
}

// Clients informa quantos clientes estão conectados.
func (h *Hub) Clients() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

// ServeHTTP atende GET /stream: envia o snapshot (e, com WithUsage, o uso)
// ao conectar, um novo a cada mudança coalescida e um heartbeat periódico, até o cliente desconectar ou
// o Hub parar.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "método não permitido", http.StatusMethodNotAllowed)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming não suportado", http.StatusInternalServerError)
		return
	}
	select {
	case <-h.done:
		http.Error(w, "servidor encerrando", http.StatusServiceUnavailable)
		return
	default:
	}

	// Registra antes de ler o snapshot inicial: uma mudança que chegue entre
	// as duas coisas gera um envio extra, nunca um envio perdido.
	cl := &sseClient{snapshot: make(chan []byte, 1), uso: make(chan []byte, 1)}
	h.mu.Lock()
	h.clients[cl] = struct{}{}
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.clients, cl)
		h.mu.Unlock()
	}()

	rc := http.NewResponseController(w)
	// Sem suporte (ResponseRecorder dos testes), os erros são ignorados: o
	// servidor sem prazos não precisa deles.
	_ = rc.SetReadDeadline(time.Time{})
	write := func(msg []byte) error {
		_ = rc.SetWriteDeadline(time.Now().Add(sseWriteWait))
		_, err := w.Write(msg)
		return err
	}

	hdr := w.Header()
	hdr.Set("Content-Type", "text/event-stream")
	hdr.Set("Cache-Control", "no-cache")
	hdr.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	msg, err := h.snapshotEvent()
	if err != nil {
		log.Printf("sse: falha ao serializar snapshot: %v", err)
		return
	}
	if err := write(msg); err != nil {
		return
	}
	if h.usage != nil {
		msg, err := h.usoEvent()
		if err != nil {
			log.Printf("sse: falha ao serializar uso: %v", err)
			return
		}
		if err := write(msg); err != nil {
			return
		}
	}
	flusher.Flush()

	ping := time.NewTicker(h.Heartbeat)
	defer ping.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-h.done:
			return
		case msg := <-cl.snapshot:
			if err := write(msg); err != nil {
				return
			}
		case msg := <-cl.uso:
			if err := write(msg); err != nil {
				return
			}
		case <-ping.C:
			if err := write([]byte(": ping\n\n")); err != nil {
				return
			}
		}
		flusher.Flush()
	}
}

// broadcast entrega msg a todos os clientes, no canal que pick escolhe, sem
// bloquear. Se o buffer está cheio, a mensagem antiga daquele tipo é
// descartada e trocada pela nova.
func (h *Hub) broadcast(msg []byte, pick func(*sseClient) chan []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for cl := range h.clients {
		ch := pick(cl)
		select {
		case ch <- msg:
			continue
		default:
		}
		select {
		case <-ch: // descarta a mensagem que o cliente ainda não leu
		default:
		}
		select {
		case ch <- msg:
		default:
		}
	}
}

// snapshotEvent monta o evento SSE "snapshot" com o estado atual em JSON.
// Lista vazia vira "[]", nunca "null".
func (h *Hub) snapshotEvent() ([]byte, error) {
	views := h.src.Snapshot()
	if views == nil {
		views = []store.SessionView{}
	}
	data, err := json.Marshal(views)
	if err != nil {
		return nil, err
	}
	return fmt.Appendf(nil, "event: snapshot\ndata: %s\n\n", data), nil
}

// usoEvent monta o evento SSE "uso" com o último uso de cada conta em JSON.
// Lista vazia vira "[]", nunca "null".
func (h *Hub) usoEvent() ([]byte, error) {
	entries := h.usage.Snapshot()
	if entries == nil {
		entries = []ratelimit.Entry{}
	}
	data, err := json.Marshal(entries)
	if err != nil {
		return nil, err
	}
	return fmt.Appendf(nil, "event: uso\ndata: %s\n\n", data), nil
}
