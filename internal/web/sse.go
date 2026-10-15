// Package web reúne o servidor HTTP do claude-fleet: página, API e stream SSE.
package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/devpolatto/claude-fleet/internal/store"
)

// Intervalos padrão do Hub.
const (
	DefaultCoalesce  = 250 * time.Millisecond
	DefaultHeartbeat = 20 * time.Second
)

// Source é o que o Hub precisa do Store: um canal de sinais de mudança
// (coalescidos, como em store.Store.Subscribe) e o snapshot atual.
type Source interface {
	Subscribe() <-chan struct{}
	Snapshot() []store.SessionView
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

	src  Source
	done chan struct{} // fechado quando Run termina

	mu      sync.Mutex
	clients map[chan []byte]struct{}
}

// NewHub cria um Hub com os intervalos padrão. Os campos Coalesce e
// Heartbeat podem ser ajustados antes de Run e de servir requisições.
func NewHub(src Source) *Hub {
	return &Hub{
		Coalesce:  DefaultCoalesce,
		Heartbeat: DefaultHeartbeat,
		src:       src,
		done:      make(chan struct{}),
		clients:   make(map[chan []byte]struct{}),
	}
}

// Run assina a Source e distribui snapshots até ctx ser cancelado. Ao sair,
// encerra todas as conexões abertas e passa a recusar novas com 503. Deve
// ser chamado uma única vez.
func (h *Hub) Run(ctx context.Context) {
	defer close(h.done)

	changes := h.src.Subscribe()
	var (
		timer   *time.Timer
		timerC  <-chan time.Time // nil enquanto não há envio pendente
		pending bool
	)
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-changes:
			if pending {
				continue // já há envio agendado; ele levará o estado mais novo
			}
			pending = true
			if timer == nil {
				timer = time.NewTimer(h.Coalesce)
			} else {
				timer.Reset(h.Coalesce)
			}
			timerC = timer.C
		case <-timerC:
			pending, timerC = false, nil
			msg, err := h.snapshotEvent()
			if err != nil {
				log.Printf("sse: falha ao serializar snapshot: %v", err)
				continue
			}
			h.broadcast(msg)
		}
	}
}

// Clients informa quantos clientes estão conectados.
func (h *Hub) Clients() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

// ServeHTTP atende GET /stream: envia o snapshot ao conectar, um novo a cada
// mudança coalescida e um heartbeat periódico, até o cliente desconectar ou
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
	ch := make(chan []byte, 1)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.clients, ch)
		h.mu.Unlock()
	}()

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
	if _, err := w.Write(msg); err != nil {
		return
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
		case msg := <-ch:
			if _, err := w.Write(msg); err != nil {
				return
			}
		case <-ping.C:
			if _, err := io.WriteString(w, ": ping\n\n"); err != nil {
				return
			}
		}
		flusher.Flush()
	}
}

// broadcast entrega msg a todos os clientes sem bloquear. Se o buffer de um
// cliente está cheio, o snapshot antigo é descartado e trocado pelo novo.
func (h *Hub) broadcast(msg []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		select {
		case ch <- msg:
			continue
		default:
		}
		select {
		case <-ch: // descarta o snapshot que o cliente ainda não leu
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
