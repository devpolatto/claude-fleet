package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/devpolatto/claude-fleet/internal/account"
	"github.com/devpolatto/claude-fleet/internal/ingest"
)

func contasDeTeste(t *testing.T) *account.List {
	t.Helper()
	l, err := account.Parse([]string{"pessoal=/home/u/.claude-pessoal", "trabalho=/home/u/.claude-trabalho"}, "")
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// evNaConta é ev(name) com o transcript dentro de <configDir>/projects.
func evNaConta(name, configDir string) ingest.Event {
	e := ev(name)
	e.TranscriptPath = filepath.Join(configDir, "projects", "-home-u-proj", sid+".jsonl")
	return e
}

func TestApplyGravaContaPeloTranscript(t *testing.T) {
	tests := []struct {
		name, configDir, want string
	}{
		{"conta pessoal", "/home/u/.claude-pessoal", "pessoal"},
		{"conta de trabalho", "/home/u/.claude-trabalho", "trabalho"},
		{"diretório não declarado", "/home/u/.claude-x", account.Outra},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New(WithAccounts(contasDeTeste(t)))
			s.Apply(evNaConta(evSessionStart, tt.configDir), at(0))
			// O hook manda o transcript da sessão também nos eventos de subagente.
			sub := evNaConta(evSubagentStart, tt.configDir)
			sub.AgentID = "ag1"
			s.Apply(sub, at(1))
			if got := mustGet(t, s).Account; got != tt.want {
				t.Errorf("Get().Account = %q, queria %q", got, tt.want)
			}
			views := s.Snapshot()
			if len(views) != 1 || views[0].Account != tt.want {
				t.Errorf("Snapshot() = %+v, queria Account %q", views, tt.want)
			}
		})
	}
}

func TestApplySemTranscriptMantemConta(t *testing.T) {
	s := New(WithAccounts(contasDeTeste(t)))
	s.Apply(evNaConta(evSessionStart, "/home/u/.claude-pessoal"), at(0))

	sem := ev(evUserPromptSubmit)
	sem.TranscriptPath = ""
	s.Apply(sem, at(1))
	if got := mustGet(t, s).Account; got != "pessoal" {
		t.Fatalf("evento sem transcript_path mudou a conta para %q", got)
	}

	// Retomada da mesma sessão em outra conta: a conta acompanha o transcript
	// e a mudança chega ao SSE.
	ch := s.Subscribe()
	if !s.Apply(evNaConta(evSessionStart, "/home/u/.claude-trabalho"), at(2)) {
		t.Fatal("Apply não informou mudança ao trocar de conta")
	}
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("troca de conta não notificou os assinantes")
	}
	if got := mustGet(t, s).Account; got != "trabalho" {
		t.Errorf("Account = %q, queria trabalho", got)
	}
}

func TestBootstrapMarcaContaPorDiretorio(t *testing.T) {
	home := t.TempDir()
	now := time.Now().Truncate(time.Second)
	recente := now.Add(-time.Minute)
	pessoal := filepath.Join(home, ".claude-pessoal")
	trabalho := filepath.Join(home, ".claude-trabalho")
	writeTranscript(t, pessoal, "projects/-a/s-pessoal.jsonl", lineUser+"\n", recente)
	writeTranscript(t, trabalho, "projects/-b/s-trabalho.jsonl", lineUser+"\n", recente)

	l, err := account.Parse([]string{"pessoal=" + pessoal, "trabalho=" + trabalho}, "")
	if err != nil {
		t.Fatal(err)
	}
	s := New(WithAccounts(l))
	for _, a := range l.All() {
		if n := s.Bootstrap(a.ProjectsDir(), now, bootMaxAge); n != 1 {
			t.Errorf("Bootstrap(%s) = %d, queria 1", a.Name, n)
		}
	}
	want := map[string]string{"s-pessoal": "pessoal", "s-trabalho": "trabalho"}
	for id, conta := range want {
		d, ok := s.Get(id)
		if !ok {
			t.Fatalf("sessão %s não encontrada", id)
		}
		if d.Account != conta {
			t.Errorf("%s: Account = %q, queria %q", id, d.Account, conta)
		}
	}
}

func TestSemContasNaoMudaNada(t *testing.T) {
	s := New()
	s.Apply(evNaConta(evSessionStart, "/home/u/.claude"), at(0))
	s.Apply(ev(evStop), at(1))
	if got := mustGet(t, s).Account; got != "" {
		t.Errorf("sem WithAccounts: Account = %q, queria vazio", got)
	}

}
