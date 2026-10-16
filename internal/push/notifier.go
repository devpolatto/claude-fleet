package push

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/devpolatto/claude-fleet/internal/store"
)

// Intervalos padrão do Notifier.
const (
	DefaultReminder = 5 * time.Minute
	DefaultTick     = 30 * time.Second
)

// Source é o que o Notifier precisa do Store.
type Source interface {
	Subscribe() <-chan struct{}
	Snapshot() []store.SessionView
}

// Sender é o que o Notifier precisa do Service.
type Sender interface {
	SendAll(ctx context.Context, msg Message, topic string) int
}

// Notifier detecta no servidor quando uma sessão ou um subagente entra em
// aguardando e envia um push, sem depender de página aberta. Enquanto ela
// seguir aguardando, reenvia a cada Reminder.
type Notifier struct {
	Reminder time.Duration
	Tick     time.Duration

	src  Source
	send Sender
	now  func() time.Time
	// dispatch roda o envio; fora dos testes, numa goroutine, para um
	// serviço de push lento não atrasar a leitura do Store.
	dispatch func(func())

	// lastSent guarda, por chave aguardando ("<sessão>" ou
	// "<sessão>/<subagente>"), quando ela foi notificada pela última vez.
	lastSent map[string]time.Time
}

// NewNotifier cria um Notifier com os intervalos padrão.
func NewNotifier(src Source, send Sender) *Notifier {
	return &Notifier{
		Reminder: DefaultReminder,
		Tick:     DefaultTick,
		src:      src,
		send:     send,
		now:      time.Now,
		dispatch: func(f func()) { go f() },
		lastSent: make(map[string]time.Time),
	}
}

// Run acompanha o Store até ctx ser cancelado.
func (n *Notifier) Run(ctx context.Context) {
	changes := n.src.Subscribe()
	t := time.NewTicker(n.Tick)
	defer t.Stop()
	n.check(ctx, n.src.Snapshot())
	for {
		select {
		case <-ctx.Done():
			return
		case <-changes:
		case <-t.C:
		}
		n.check(ctx, n.src.Snapshot())
	}
}

// check compara o snapshot com o que já foi notificado. Uma sessão recebe no
// máximo um push por passada, mesmo com ela e subagentes aguardando juntos.
func (n *Notifier) check(ctx context.Context, views []store.SessionView) {
	now := n.now()
	waiting := make(map[string]bool)
	for _, v := range views {
		var due, selfWaiting bool
		keys := waitingKeys(v)
		for _, k := range keys {
			waiting[k] = true
			last, seen := n.lastSent[k]
			if !seen || now.Sub(last) >= n.Reminder {
				due = true
			}
			if k == v.ID {
				selfWaiting = true
			}
		}
		if !due {
			continue
		}
		for _, k := range keys {
			n.lastSent[k] = now
		}
		msg := messageFor(v, selfWaiting)
		topic := topicFor(v.ID)
		n.dispatch(func() { n.send.SendAll(ctx, msg, topic) })
	}
	for k := range n.lastSent {
		if !waiting[k] {
			delete(n.lastSent, k) // saiu de aguardando: o lembrete para
		}
	}
}

// waitingKeys lista as chaves em aguardando de uma sessão.
func waitingKeys(v store.SessionView) []string {
	var keys []string
	if v.Status == store.StatusAguardando {
		keys = append(keys, v.ID)
	}
	for _, a := range v.Subagents {
		if a.Status == store.StatusAguardando {
			keys = append(keys, v.ID+"/"+a.ID)
		}
	}
	return keys
}

// messageFor diz só qual sessão e que ela aguarda: a notificação aparece na
// tela bloqueada, então nada do trabalho (descrição, ferramenta) vai junto.
func messageFor(v store.SessionView, selfWaiting bool) Message {
	name := v.Name
	if name == "" {
		name = v.Project
	}
	if name == "" {
		name = v.ShortID
	}
	body := name + " · aguardando permissão"
	if !selfWaiting {
		body = name + " · subagente aguardando"
	}
	return Message{Title: "Claude Fleet", Body: body, Tag: "fleet-" + v.ID, URL: "/"}
}

// topicFor gera o Topic da sessão: 32 caracteres hex, dentro do alfabeto
// base64url exigido, sem expor o ID.
func topicFor(sessionID string) string {
	sum := sha256.Sum256([]byte(sessionID))
	return hex.EncodeToString(sum[:16])
}
