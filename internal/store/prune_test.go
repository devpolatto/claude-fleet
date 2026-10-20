package store

import (
	"strconv"
	"testing"
	"time"

	"github.com/devpolatto/claude-fleet/internal/ingest"
)

// pruneStore monta um Store aplicando os eventos em sequência, um por
// instante, todos relativos a t0.
func pruneStore(t *testing.T, steps ...pruneStep) *Store {
	t.Helper()
	s := New()
	for _, st := range steps {
		s.Apply(st.ev, t0.Add(st.at))
	}
	return s
}

type pruneStep struct {
	at time.Duration
	ev ingest.Event
}

func pruneHas(s *Store) bool {
	_, ok := s.Get(sid)
	return ok
}

func TestPruneEncerrada(t *testing.T) {
	end := 10 * time.Minute // SessionEnd em t0 + 10 min
	tests := []struct {
		name  string
		after time.Duration // tempo desde o SessionEnd
		gone  bool
	}{
		{"há 14:59 fica", 14*time.Minute + 59*time.Second, false},
		{"há exatamente 15:00 fica", 15 * time.Minute, false},
		{"há 15:01 sai", 15*time.Minute + time.Second, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := pruneStore(t,
				pruneStep{0, ev("SessionStart")},
				pruneStep{time.Minute, tool("PreToolUse", "Bash")},
				pruneStep{end, ev("SessionEnd")},
			)
			changed := s.Prune(t0.Add(end + tt.after))
			if changed != tt.gone {
				t.Errorf("changed = %v, quer %v", changed, tt.gone)
			}
			if got := pruneHas(s); got == tt.gone {
				t.Errorf("sessão presente = %v, quer %v", got, !tt.gone)
			}
		})
	}
}

func TestPruneInativa(t *testing.T) {
	last := 5 * time.Minute // último evento em t0 + 5 min
	base := []pruneStep{
		{0, ev("SessionStart")},
		{time.Minute, tool("PreToolUse", "Bash")},
		{last, ev("Stop")},
	}
	tests := []struct {
		name  string
		after time.Duration // tempo desde o último evento
		gone  bool
	}{
		{"há 11:59 h fica", 11*time.Hour + 59*time.Minute, false},
		{"há exatamente 12 h fica", 12 * time.Hour, false},
		{"há 12:01 h sai", 12*time.Hour + time.Minute, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := pruneStore(t, base...)
			changed := s.Prune(t0.Add(last + tt.after))
			if changed != tt.gone {
				t.Errorf("changed = %v, quer %v", changed, tt.gone)
			}
			if got := pruneHas(s); got == tt.gone {
				t.Errorf("sessão presente = %v, quer %v", got, !tt.gone)
			}
		})
	}
}

// A regra das 12 h vale para qualquer status, inclusive ativo e aguardando.
func TestPruneInativaQualquerStatus(t *testing.T) {
	for _, last := range []ingest.Event{tool("PreToolUse", "Bash"), ev("PermissionRequest"), ev("PreCompact")} {
		t.Run(last.Name, func(t *testing.T) {
			s := pruneStore(t, pruneStep{0, ev("SessionStart")}, pruneStep{time.Second, last})
			if !s.Prune(t0.Add(time.Second + 12*time.Hour + time.Minute)) {
				t.Fatalf("status %s: sessão inativa há 12:01 h não saiu", mustGet(t, s).Status)
			}
		})
	}
}

// Sessão reaberta não tem EndedAt e não sai pela regra dos 15 min.
func TestPruneReabertaNaoSai(t *testing.T) {
	for _, reopen := range []string{"UserPromptSubmit", "SessionStart"} {
		t.Run(reopen, func(t *testing.T) {
			s := pruneStore(t,
				pruneStep{0, ev("SessionStart")},
				pruneStep{time.Minute, ev("SessionEnd")},
				pruneStep{2 * time.Minute, ev(reopen)},
			)
			if d := mustGet(t, s); d.EndedAt != nil || d.Status == StatusEncerrado {
				t.Fatalf("sessão não reabriu: status %s, EndedAt %v", d.Status, d.EndedAt)
			}
			if s.Prune(t0.Add(time.Hour)) {
				t.Error("changed = true para sessão reaberta")
			}
			if !pruneHas(s) {
				t.Error("sessão reaberta removida pela regra dos 15 min")
			}
		})
	}
}

// Evento atrasado depois do SessionEnd não reabre a sessão nem adia a
// regra dos 15 min, que conta a partir de EndedAt.
func TestPruneEncerradaComEventoAtrasado(t *testing.T) {
	s := pruneStore(t,
		pruneStep{0, ev("SessionStart")},
		pruneStep{time.Minute, ev("SessionEnd")},
		pruneStep{10 * time.Minute, tool("PostToolUse", "Bash")},
	)
	if !s.Prune(t0.Add(time.Minute + 15*time.Minute + time.Second)) {
		t.Fatal("encerrada há 15:01 com evento atrasado não saiu")
	}
}

// A regra das 12 h usa o maior LastEventAt entre sessão e subagentes.
func TestPruneSubagente(t *testing.T) {
	t.Run("subagente mais recente segura a sessão", func(t *testing.T) {
		s := pruneStore(t,
			pruneStep{0, ev("SessionStart")},
			pruneStep{time.Minute, agentEv("SubagentStart", "a1")},
		)
		// Estado montado por fora do Apply (ex.: descoberta no boot):
		// o subagente tem evento mais novo que a sessão.
		s.sessions[sid].LastEventAt = t0
		s.sessions[sid].Subagents["a1"].LastEventAt = t0.Add(time.Hour)

		if s.Prune(t0.Add(12*time.Hour + time.Minute)) {
			t.Error("sessão removida com subagente ativo há 11:01 h")
		}
		if !s.Prune(t0.Add(13*time.Hour + time.Minute)) {
			t.Error("sessão não saiu com o subagente inativo há 12:01 h")
		}
	})
	t.Run("subagente ativo sem eventos não segura", func(t *testing.T) {
		s := pruneStore(t,
			pruneStep{0, ev("SessionStart")},
			pruneStep{time.Minute, agentEv("SubagentStart", "a1")},
		)
		if !s.Prune(t0.Add(time.Minute + 12*time.Hour + time.Minute)) {
			t.Error("subagente ativo, mas sem eventos há 12:01 h, segurou a sessão")
		}
	})
}

func TestPruneSoRemoveAsVencidas(t *testing.T) {
	s := New()
	other := ev("SessionStart")
	other.SessionID = "outra-sessao"
	s.Apply(ev("SessionStart"), t0)
	s.Apply(ev("SessionEnd"), t0)
	s.Apply(other, t0.Add(10*time.Minute))

	if !s.Prune(t0.Add(16 * time.Minute)) {
		t.Fatal("changed = false com uma sessão vencida")
	}
	if pruneHas(s) {
		t.Error("sessão encerrada há 16 min continua")
	}
	if _, ok := s.Get("outra-sessao"); !ok {
		t.Error("sessão recente foi removida")
	}
}

func TestPruneNadaSai(t *testing.T) {
	s := pruneStore(t, pruneStep{0, ev("SessionStart")})
	ch := s.Subscribe()
	if s.Prune(t0.Add(time.Hour)) {
		t.Error("changed = true sem nada para remover")
	}
	if New().Prune(t0) {
		t.Error("changed = true com Store vazio")
	}
	select {
	case <-ch:
		t.Error("sinal no Subscribe sem remoção")
	default:
	}
}

func TestPruneSinaliza(t *testing.T) {
	s := pruneStore(t, pruneStep{0, ev("SessionStart")}, pruneStep{0, ev("SessionEnd")})
	ch := s.Subscribe()
	if !s.Prune(t0.Add(time.Hour)) {
		t.Fatal("changed = false")
	}
	select {
	case <-ch:
	default:
		t.Error("nenhum sinal no Subscribe após remoção")
	}
	if len(s.Snapshot()) != 0 {
		t.Error("Snapshot ainda traz a sessão removida")
	}
}

// Com o mapa cheio, a sessão nova entra e sai a de atividade mais antiga.
func TestSessaoNovaComMapaCheioTiraAMaisAntiga(t *testing.T) {
	s := New()
	for i := 0; i < maxSessions; i++ {
		s.Apply(ingest.Event{Name: "SessionStart", SessionID: "s" + strconv.Itoa(i)}, t0.Add(time.Duration(i)*time.Second))
	}
	// s0 é a mais antiga, mas recebe um evento agora: a mais antiga passa a ser s1.
	s.Apply(ingest.Event{Name: "Stop", SessionID: "s0"}, t0.Add(time.Hour))
	s.Apply(ingest.Event{Name: "SessionStart", SessionID: "nova"}, t0.Add(time.Hour))

	if n := len(s.Snapshot()); n != maxSessions {
		t.Errorf("sessões = %d, queria %d", n, maxSessions)
	}
	for id, want := range map[string]bool{"nova": true, "s0": true, "s1": false, "s2": true} {
		if _, ok := s.Get(id); ok != want {
			t.Errorf("%s presente = %t, queria %t", id, ok, want)
		}
	}
	// Evento de sessão já conhecida não tira ninguém.
	s.Apply(ingest.Event{Name: "Stop", SessionID: "s2"}, t0.Add(2*time.Hour))
	if n := len(s.Snapshot()); n != maxSessions {
		t.Errorf("depois de evento conhecido: sessões = %d, queria %d", n, maxSessions)
	}
}
