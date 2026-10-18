package web

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/devpolatto/claude-fleet/internal/ingest"
	"github.com/devpolatto/claude-fleet/internal/ratelimit"
	"github.com/devpolatto/claude-fleet/internal/store"
)

// sseMsg é uma mensagem lida do stream: um evento nomeado ou um comentário.
type sseMsg struct {
	event, data, comment string
}

// client é uma conexão de teste ao /stream.
type client struct {
	msgs   chan sseMsg
	cancel context.CancelFunc
	done   chan struct{} // fechado quando a leitura termina (EOF ou erro)
}

// connect abre GET /stream e lê as mensagens SSE numa goroutine.
func connect(t *testing.T, url string) *client {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		cancel()
		t.Fatalf("status = %d, quero 200", resp.StatusCode)
	}
	for k, v := range map[string]string{
		"Content-Type":      "text/event-stream",
		"Cache-Control":     "no-cache",
		"X-Accel-Buffering": "no",
	} {
		if got := resp.Header.Get(k); got != v {
			t.Errorf("header %s = %q, quero %q", k, got, v)
		}
	}

	c := &client{msgs: make(chan sseMsg, 64), cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(c.done)
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
		var m sseMsg
		for sc.Scan() {
			line := sc.Text()
			switch {
			case line == "":
				select {
				case c.msgs <- m:
				case <-ctx.Done():
					return
				}
				m = sseMsg{}
			case strings.HasPrefix(line, ":"):
				m.comment = strings.TrimSpace(line[1:])
			case strings.HasPrefix(line, "event: "):
				m.event = line[len("event: "):]
			case strings.HasPrefix(line, "data: "):
				m.data = line[len("data: "):]
			}
		}
	}()
	t.Cleanup(cancel)
	return c
}

// next devolve a próxima mensagem ou falha após o timeout.
func (c *client) next(t *testing.T, timeout time.Duration) sseMsg {
	t.Helper()
	select {
	case m := <-c.msgs:
		return m
	case <-time.After(timeout):
		t.Fatalf("nenhuma mensagem em %v", timeout)
		return sseMsg{}
	}
}

// nextSnapshot pula heartbeats e devolve as sessões do próximo snapshot.
func (c *client) nextSnapshot(t *testing.T, timeout time.Duration) []store.SessionView {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		m := c.next(t, time.Until(deadline))
		if m.event != "snapshot" {
			continue
		}
		var views []store.SessionView
		if err := json.Unmarshal([]byte(m.data), &views); err != nil {
			t.Fatalf("snapshot com JSON inválido %q: %v", m.data, err)
		}
		if views == nil {
			t.Fatalf("snapshot deveria ser lista, veio %q", m.data)
		}
		return views
	}
}

// startHub sobe um Hub sobre st com intervalos de teste e um servidor
// httptest. Devolve o Hub, a URL e a função que para o laço do Hub.
func startHub(t *testing.T, st *store.Store, coalesce, heartbeat time.Duration, opts ...HubOption) (*Hub, string, context.CancelFunc) {
	t.Helper()
	h := NewHub(st, opts...)
	h.Coalesce = coalesce
	h.Heartbeat = heartbeat
	ctx, cancel := context.WithCancel(context.Background())
	go h.Run(ctx)
	srv := httptest.NewServer(h)
	t.Cleanup(func() {
		cancel()
		srv.Close()
	})
	return h, srv.URL + "/stream", cancel
}

func sessionStart(id string) ingest.Event {
	return ingest.Event{Name: "SessionStart", Source: "startup", SessionID: id, Cwd: "/tmp/projeto"}
}

// waitFor espera cond ficar verdadeira, conferindo a cada 5 ms.
func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("tempo esgotado esperando %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestStreamSnapshotInicialEAposMudanca(t *testing.T) {
	st := store.New()
	st.Apply(sessionStart("sessao-a"), time.Now())
	_, url, _ := startHub(t, st, 20*time.Millisecond, time.Hour)

	c := connect(t, url)
	views := c.nextSnapshot(t, time.Second)
	if len(views) != 1 || views[0].ID != "sessao-a" {
		t.Fatalf("snapshot inicial = %+v, quero só sessao-a", views)
	}

	st.Apply(sessionStart("sessao-b"), time.Now())
	views = c.nextSnapshot(t, time.Second)
	if len(views) != 2 {
		t.Fatalf("snapshot após Apply tem %d sessões, quero 2", len(views))
	}
}

func TestStreamSnapshotInicialVazioEhLista(t *testing.T) {
	_, url, _ := startHub(t, store.New(), 20*time.Millisecond, time.Hour)
	c := connect(t, url)
	if views := c.nextSnapshot(t, time.Second); len(views) != 0 {
		t.Fatalf("snapshot = %+v, quero lista vazia", views)
	}
}

func TestStreamCoalesceMudancasSeguidas(t *testing.T) {
	st := store.New()
	// Janela padrão de 250 ms: 10 mudanças em 100 ms cabem numa janela só.
	_, url, _ := startHub(t, st, DefaultCoalesce, time.Hour)
	c := connect(t, url)
	c.nextSnapshot(t, time.Second) // inicial

	for i := range 10 {
		st.Apply(sessionStart(fmt.Sprintf("sessao-%02d", i)), time.Now())
		time.Sleep(10 * time.Millisecond)
	}

	// Espera duas janelas inteiras e conta os snapshots que chegaram.
	var envios int
	var last []store.SessionView
	timeout := time.After(2*DefaultCoalesce + 200*time.Millisecond)
loop:
	for {
		select {
		case m := <-c.msgs:
			if m.event == "snapshot" {
				envios++
				if err := json.Unmarshal([]byte(m.data), &last); err != nil {
					t.Fatal(err)
				}
			}
		case <-timeout:
			break loop
		}
	}
	if envios < 1 || envios > 2 {
		t.Fatalf("10 mudanças em 100 ms geraram %d envios, quero 1 ou 2", envios)
	}
	if len(last) != 10 {
		t.Fatalf("último snapshot tem %d sessões, quero 10", len(last))
	}
}

func TestStreamClienteDesconectadoEhRemovido(t *testing.T) {
	st := store.New()
	h, url, _ := startHub(t, st, 10*time.Millisecond, 10*time.Millisecond)

	base := runtime.NumGoroutine()

	clientes := make([]*client, 5)
	for i := range clientes {
		clientes[i] = connect(t, url)
		clientes[i].nextSnapshot(t, time.Second)
	}
	waitFor(t, time.Second, "5 clientes", func() bool { return h.Clients() == 5 })

	// Desconecta 3; os outros 2 continuam recebendo.
	for _, c := range clientes[:3] {
		c.cancel()
		<-c.done
	}
	waitFor(t, time.Second, "2 clientes", func() bool { return h.Clients() == 2 })

	st.Apply(sessionStart("depois"), time.Now())
	for _, c := range clientes[3:] {
		if views := c.nextSnapshot(t, time.Second); len(views) != 1 {
			t.Fatalf("cliente restante recebeu %d sessões, quero 1", len(views))
		}
	}

	for _, c := range clientes[3:] {
		c.cancel()
		<-c.done
	}
	waitFor(t, time.Second, "0 clientes", func() bool { return h.Clients() == 0 })

	// Sem vazamento: as goroutines voltam perto da linha de base (com folga
	// para conexões ociosas do transporte HTTP que ainda estejam fechando).
	http.DefaultClient.CloseIdleConnections()
	waitFor(t, 2*time.Second, "goroutines voltarem à linha de base", func() bool {
		return runtime.NumGoroutine() <= base+2
	})
}

func TestStreamClienteLentoNaoTravaOsOutros(t *testing.T) {
	st := store.New()
	h, url, _ := startHub(t, st, 5*time.Millisecond, time.Hour)

	lento := connect(t, url) // nunca é lido depois do inicial
	lento.nextSnapshot(t, time.Second)
	rapido := connect(t, url)
	rapido.nextSnapshot(t, time.Second)

	for i := range 50 {
		st.Apply(sessionStart(fmt.Sprintf("s-%02d", i)), time.Now())
		time.Sleep(time.Millisecond)
	}
	waitFor(t, 2*time.Second, "cliente rápido ver as 50 sessões", func() bool {
		select {
		case m := <-rapido.msgs:
			var v []store.SessionView
			_ = json.Unmarshal([]byte(m.data), &v)
			return len(v) == 50
		default:
			return false
		}
	})
	if h.Clients() != 2 {
		t.Fatalf("clientes = %d, quero 2", h.Clients())
	}
}

func TestStreamHeartbeat(t *testing.T) {
	_, url, _ := startHub(t, store.New(), 10*time.Millisecond, 20*time.Millisecond)
	c := connect(t, url)
	c.nextSnapshot(t, time.Second)
	for range 2 {
		m := c.next(t, time.Second)
		if m.comment != "ping" || m.event != "" {
			t.Fatalf("mensagem = %+v, quero comentário ping", m)
		}
	}
}

func TestStreamRunCanceladoEncerraConexoes(t *testing.T) {
	h, url, stop := startHub(t, store.New(), 10*time.Millisecond, time.Hour)
	c := connect(t, url)
	c.nextSnapshot(t, time.Second)

	stop()
	select {
	case <-c.done:
	case <-time.After(time.Second):
		t.Fatal("conexão continuou aberta após cancelar Run")
	}
	waitFor(t, time.Second, "0 clientes", func() bool { return h.Clients() == 0 })

	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status após parar = %d, quero 503", resp.StatusCode)
	}
}

func TestStreamMetodoNaoPermitido(t *testing.T) {
	_, url, _ := startHub(t, store.New(), 10*time.Millisecond, time.Hour)
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		req, _ := http.NewRequest(m, url, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("%s: status = %d, quero 405", m, resp.StatusCode)
		}
		if got := resp.Header.Get("Allow"); got != http.MethodGet {
			t.Errorf("%s: Allow = %q, quero GET", m, got)
		}
	}
}

// fakeUsage é um UsageSource controlado pelo teste, com o mesmo contrato do
// ratelimit.Tracker: canal com buffer 1 e aviso que não bloqueia.
type fakeUsage struct {
	mu      sync.Mutex
	entries []ratelimit.Entry
	ch      chan struct{}
}

func newFakeUsage(entries []ratelimit.Entry) *fakeUsage {
	return &fakeUsage{entries: entries, ch: make(chan struct{}, 1)}
}

func (f *fakeUsage) Subscribe() <-chan struct{} { return f.ch }

func (f *fakeUsage) Snapshot() []ratelimit.Entry {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.entries
}

func (f *fakeUsage) set(entries []ratelimit.Entry) {
	f.mu.Lock()
	f.entries = entries
	f.mu.Unlock()
	select {
	case f.ch <- struct{}{}:
	default:
	}
}

// nextEvent pula heartbeats e devolve o próximo evento nomeado.
func (c *client) nextEvent(t *testing.T, timeout time.Duration) sseMsg {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		m := c.next(t, time.Until(deadline))
		if m.event != "" {
			return m
		}
	}
}

// semEvento falha se algum evento nomeado chegar dentro de d.
func (c *client) semEvento(t *testing.T, d time.Duration) {
	t.Helper()
	timeout := time.After(d)
	for {
		select {
		case m := <-c.msgs:
			if m.event != "" {
				t.Fatalf("evento inesperado %q: %s", m.event, m.data)
			}
		case <-timeout:
			return
		}
	}
}

// decodeUso lê o data de um evento "uso" e confere que é uma lista.
func decodeUso(t *testing.T, m sseMsg) []ratelimit.Entry {
	t.Helper()
	if m.event != "uso" {
		t.Fatalf("evento = %q, quero uso", m.event)
	}
	var entries []ratelimit.Entry
	if err := json.Unmarshal([]byte(m.data), &entries); err != nil {
		t.Fatalf("uso com JSON inválido %q: %v", m.data, err)
	}
	if entries == nil {
		t.Fatalf("uso deveria ser lista, veio %q", m.data)
	}
	return entries
}

func usoConta(conta string, pct float64) ratelimit.Entry {
	return ratelimit.Entry{
		Account:    conta,
		FiveHour:   &ratelimit.Window{UsedPercentage: pct, ResetsAt: 1_800_000_000},
		ReceivedAt: time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC),
	}
}

func TestHubEnviaUsoAoConectar(t *testing.T) {
	casos := []struct {
		nome     string
		entries  []ratelimit.Entry
		wantData string // "" confere só as contas
		contas   []string
	}{
		{nome: "nil vira lista vazia", entries: nil, wantData: "[]"},
		{nome: "vazia vira lista vazia", entries: []ratelimit.Entry{}, wantData: "[]"},
		{nome: "duas contas", entries: []ratelimit.Entry{usoConta("pessoal", 23.5), usoConta("trabalho", 80)}, contas: []string{"pessoal", "trabalho"}},
	}
	for _, tc := range casos {
		t.Run(tc.nome, func(t *testing.T) {
			st := store.New()
			st.Apply(sessionStart("sessao-a"), time.Now())
			_, url, _ := startHub(t, st, 20*time.Millisecond, time.Hour, WithUsage(newFakeUsage(tc.entries)))
			c := connect(t, url)

			// O snapshot vem primeiro, e o uso logo depois.
			if m := c.nextEvent(t, time.Second); m.event != "snapshot" {
				t.Fatalf("1º evento = %q, quero snapshot", m.event)
			}
			m := c.nextEvent(t, time.Second)
			entries := decodeUso(t, m)
			if tc.wantData != "" && m.data != tc.wantData {
				t.Fatalf("data = %q, quero %q", m.data, tc.wantData)
			}
			if len(entries) != len(tc.contas) {
				t.Fatalf("uso tem %d contas, quero %d", len(entries), len(tc.contas))
			}
			for i, conta := range tc.contas {
				if entries[i].Account != conta || entries[i].FiveHour == nil {
					t.Errorf("conta %d = %+v, quero %s com janela de 5 h", i, entries[i], conta)
				}
			}
		})
	}
}

func TestHubMudancaDeUsoNaoReenviaSnapshot(t *testing.T) {
	const coalesce = 20 * time.Millisecond
	st := store.New()
	uso := newFakeUsage(nil)
	_, url, _ := startHub(t, st, coalesce, time.Hour, WithUsage(uso))
	c := connect(t, url)
	c.nextSnapshot(t, time.Second)
	decodeUso(t, c.nextEvent(t, time.Second))

	// Mudança de uso: chega em Coalesce + folga, sem snapshot junto.
	uso.set([]ratelimit.Entry{usoConta("pessoal", 42)})
	entries := decodeUso(t, c.nextEvent(t, coalesce+500*time.Millisecond))
	if len(entries) != 1 || entries[0].FiveHour.UsedPercentage != 42 {
		t.Fatalf("uso = %+v, quero pessoal com 42%%", entries)
	}
	c.semEvento(t, 5*coalesce)

	// Mudança de sessão: chega o snapshot, sem uso junto.
	st.Apply(sessionStart("sessao-b"), time.Now())
	if views := c.nextSnapshot(t, time.Second); len(views) != 1 {
		t.Fatalf("snapshot tem %d sessões, quero 1", len(views))
	}
	c.semEvento(t, 5*coalesce)
}

func TestHubUsoNaoTomaOLugarDoSnapshotNoClienteLento(t *testing.T) {
	st := store.New()
	uso := newFakeUsage(nil)
	h, url, _ := startHub(t, st, 5*time.Millisecond, time.Hour, WithUsage(uso))
	c := connect(t, url)
	c.nextSnapshot(t, time.Second)
	decodeUso(t, c.nextEvent(t, time.Second))

	// As duas mudanças chegam antes de o cliente ler qualquer uma: cada tipo
	// tem o seu lugar, e nenhuma se perde.
	h.broadcast([]byte("event: snapshot\ndata: []\n\n"), func(cl *sseClient) chan []byte { return cl.snapshot })
	h.broadcast([]byte("event: uso\ndata: []\n\n"), func(cl *sseClient) chan []byte { return cl.uso })
	vistos := map[string]bool{}
	for range 2 {
		vistos[c.nextEvent(t, time.Second).event] = true
	}
	if !vistos["snapshot"] || !vistos["uso"] {
		t.Fatalf("eventos recebidos = %v, quero snapshot e uso", vistos)
	}
}

func TestHubSemUsageNaoMandaUso(t *testing.T) {
	const coalesce = 20 * time.Millisecond
	st := store.New()
	_, url, _ := startHub(t, st, coalesce, time.Hour)
	c := connect(t, url)
	c.nextSnapshot(t, time.Second)
	c.semEvento(t, 5*coalesce)

	st.Apply(sessionStart("sessao-a"), time.Now())
	c.nextSnapshot(t, time.Second)
	c.semEvento(t, 5*coalesce)
}
