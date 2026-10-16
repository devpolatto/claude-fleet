package push

import (
	"context"
	"testing"
	"time"

	"github.com/devpolatto/claude-fleet/internal/store"
)

type envio struct {
	msg   Message
	topic string
}

type senderFalso struct{ envios []envio }

func (s *senderFalso) SendAll(_ context.Context, msg Message, topic string) int {
	s.envios = append(s.envios, envio{msg, topic})
	return 1
}

func novoNotifier() (*Notifier, *senderFalso, *time.Time) {
	sf := &senderFalso{}
	n := NewNotifier(nil, sf)
	agora := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	n.now = func() time.Time { return agora }
	n.dispatch = func(f func()) { f() }
	return n, sf, &agora
}

func sessao(id, name string, st store.Status, subs ...store.AgentView) store.SessionView {
	return store.SessionView{ID: id, ShortID: id, Name: name, Project: "proj-" + id, Status: st, Subagents: subs}
}

func sub(id string, st store.Status) store.AgentView { return store.AgentView{ID: id, Status: st} }

func TestNotifierEntradaELembrete(t *testing.T) {
	n, sf, agora := novoNotifier()
	ctx := context.Background()
	toni := func(st store.Status) []store.SessionView {
		return []store.SessionView{sessao("t1", "toni-voz", st), sessao("f1", "", store.StatusAtivo)}
	}

	n.check(ctx, toni(store.StatusParado))
	if len(sf.envios) != 0 {
		t.Fatalf("nada aguardando: %d envios", len(sf.envios))
	}
	n.check(ctx, toni(store.StatusAguardando))
	if len(sf.envios) != 1 {
		t.Fatalf("entrada em aguardando: %d envios, queria 1", len(sf.envios))
	}
	e := sf.envios[0]
	if e.msg.Body != "toni-voz · aguardando permissão" || e.msg.Tag != "fleet-t1" || e.msg.URL != "/" || len(e.topic) != 32 {
		t.Errorf("envio = %+v", e)
	}

	n.check(ctx, toni(store.StatusAguardando)) // mesmo snapshot de novo
	*agora = agora.Add(4*time.Minute + 59*time.Second)
	n.check(ctx, toni(store.StatusAguardando))
	if len(sf.envios) != 1 {
		t.Errorf("antes de 5 min: %d envios, queria 1", len(sf.envios))
	}
	*agora = agora.Add(time.Second)
	n.check(ctx, toni(store.StatusAguardando))
	if len(sf.envios) != 2 {
		t.Errorf("aos 5 min: %d envios, queria 2 (lembrete)", len(sf.envios))
	}

	n.check(ctx, toni(store.StatusAtivo))
	*agora = agora.Add(time.Hour)
	n.check(ctx, toni(store.StatusAtivo))
	if len(sf.envios) != 2 {
		t.Errorf("saiu de aguardando: %d envios, queria 2 (lembrete parou)", len(sf.envios))
	}
	n.check(ctx, toni(store.StatusAguardando))
	if len(sf.envios) != 3 {
		t.Errorf("nova entrada: %d envios, queria 3", len(sf.envios))
	}
}

func TestNotifierSubagenteEUmPushPorSessao(t *testing.T) {
	n, sf, _ := novoNotifier()
	ctx := context.Background()

	n.check(ctx, []store.SessionView{sessao("s1", "", store.StatusAtivo, sub("a1", store.StatusAguardando))})
	if len(sf.envios) != 1 || sf.envios[0].msg.Body != "proj-s1 · subagente aguardando" {
		t.Fatalf("subagente: %+v", sf.envios)
	}
	// Sessão e outro subagente entram juntos: um push só, como sessão.
	n.check(ctx, []store.SessionView{sessao("s1", "", store.StatusAguardando, sub("a1", store.StatusAguardando), sub("a2", store.StatusAguardando))})
	if len(sf.envios) != 2 || sf.envios[1].msg.Body != "proj-s1 · aguardando permissão" {
		t.Errorf("entrada conjunta: %+v", sf.envios)
	}
	// Duas sessões diferentes: um push cada, com tags distintas.
	n.check(ctx, []store.SessionView{
		sessao("s1", "", store.StatusAguardando, sub("a1", store.StatusAguardando), sub("a2", store.StatusAguardando)),
		sessao("s2", "outra", store.StatusAguardando),
	})
	if len(sf.envios) != 3 || sf.envios[2].msg.Tag != "fleet-s2" {
		t.Errorf("segunda sessão: %+v", sf.envios)
	}
}
