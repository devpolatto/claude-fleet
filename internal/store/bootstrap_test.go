package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/devpolatto/claude-fleet/internal/ingest"
)

const bootMaxAge = 30 * time.Minute

// writeTranscript cria <dir>/<rel> com o conteúdo dado e mtime = mtime.
func writeTranscript(t *testing.T, dir, rel, content string, mtime time.Time) string {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	return p
}

// Linhas sintéticas no formato do transcript (só os campos que interessam).
const (
	lineSnapshot = `{"type":"file-history-snapshot","messageId":"m0"}`
	lineUser     = `{"type":"user","sessionId":"s1","cwd":"/home/u/meu-proj","timestamp":"2026-10-15T11:40:00.000Z","message":{"role":"user","content":"segredo"}}`
)

func TestBootstrapJanelaDeTempo(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().Truncate(time.Second)
	recente := now.Add(-10 * time.Minute)
	writeTranscript(t, dir, "-home-u-meu-proj/s1.jsonl", lineSnapshot+"\n"+lineUser+"\n", recente)
	writeTranscript(t, dir, "-home-u-velho/s2.jsonl", lineUser+"\n", now.Add(-40*time.Minute))

	s := New()
	ch := s.Subscribe()
	if got := s.Bootstrap(dir, now, bootMaxAge); got != 1 {
		t.Fatalf("added = %d, quer 1", got)
	}
	select {
	case <-ch:
	default:
		t.Error("Bootstrap que adicionou sessão não notificou")
	}

	d, ok := s.Get("s1")
	if !ok {
		t.Fatal("sessão s1 (10 min) não foi criada")
	}
	if d.Status != StatusDesconhecido {
		t.Errorf("status = %q, quer desconhecido", d.Status)
	}
	if d.Cwd != "/home/u/meu-proj" || d.Project != "meu-proj" {
		t.Errorf("Cwd/Project = %q/%q, quer /home/u/meu-proj/meu-proj", d.Cwd, d.Project)
	}
	if want := filepath.Join(dir, "-home-u-meu-proj", "s1.jsonl"); d.TranscriptPath != want {
		t.Errorf("TranscriptPath = %q, quer %q", d.TranscriptPath, want)
	}
	if !d.LastEventAt.Equal(recente) {
		t.Errorf("LastEventAt = %v, quer mtime %v", d.LastEventAt, recente)
	}
	if want := time.Date(2026, 10, 15, 11, 40, 0, 0, time.UTC); !d.StartedAt.Equal(want) {
		t.Errorf("StartedAt = %v, quer primeiro timestamp %v", d.StartedAt, want)
	}
	if _, ok := s.Get("s2"); ok {
		t.Error("sessão s2 (40 min) não deveria ter sido criada")
	}
}

func TestBootstrapSemCwdUsaMtime(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().Truncate(time.Second)
	mtime := now.Add(-5 * time.Minute)
	writeTranscript(t, dir, "p/s1.jsonl", lineSnapshot+"\nnão é json\n", mtime)
	writeTranscript(t, dir, "p/vazio.jsonl", "", mtime)

	s := New()
	if got := s.Bootstrap(dir, now, bootMaxAge); got != 2 {
		t.Fatalf("added = %d, quer 2", got)
	}
	for _, id := range []string{"s1", "vazio"} {
		d, _ := s.Get(id)
		if d.Cwd != "" || d.Project != "" {
			t.Errorf("%s: Cwd/Project = %q/%q, quer vazios", id, d.Cwd, d.Project)
		}
		if !d.StartedAt.Equal(mtime) || !d.LastEventAt.Equal(mtime) {
			t.Errorf("%s: StartedAt/LastEventAt = %v/%v, quer mtime %v", id, d.StartedAt, d.LastEventAt, mtime)
		}
	}
}

func TestBootstrapCwdAlemDoLimiteDeLinhas(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	content := strings.Repeat(lineSnapshot+"\n", bootMaxLines) + lineUser + "\n"
	writeTranscript(t, dir, "p/s1.jsonl", content, now)

	s := New()
	s.Bootstrap(dir, now, bootMaxAge)
	if d, _ := s.Get("s1"); d.Cwd != "" {
		t.Errorf("Cwd = %q; não deveria ler além de %d linhas", d.Cwd, bootMaxLines)
	}
}

func TestBootstrapIgnoraSubagentesEOutros(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	writeTranscript(t, dir, "p/s1.jsonl", lineUser+"\n", now)
	writeTranscript(t, dir, "p/s1/subagents/agent-a1.jsonl", lineUser+"\n", now)
	writeTranscript(t, dir, "p/s1/subagents/agent-a1.meta.json", `{}`, now)
	writeTranscript(t, dir, "p/notas.txt", "x", now)
	writeTranscript(t, dir, "solto.jsonl", lineUser+"\n", now) // direto em projects/
	if err := os.MkdirAll(filepath.Join(dir, "p", "pasta.jsonl"), 0o755); err != nil {
		t.Fatal(err)
	}

	s := New()
	if got := s.Bootstrap(dir, now, bootMaxAge); got != 1 {
		t.Fatalf("added = %d, quer 1", got)
	}
	snap := s.Snapshot()
	if len(snap) != 1 || snap[0].ID != "s1" {
		t.Fatalf("sessões = %+v, quer só s1", snap)
	}
	if len(snap[0].Subagents) != 0 {
		t.Errorf("subagentes = %d, quer 0", len(snap[0].Subagents))
	}
}

func TestBootstrapNaoSobrescreveSessaoConhecida(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	writeTranscript(t, dir, "p/"+sid+".jsonl", lineUser+"\n", now)

	s := New()
	seed(s, StatusAtivo)
	before := mustGet(t, s)
	ch := s.Subscribe()

	if got := s.Bootstrap(dir, now, bootMaxAge); got != 0 {
		t.Fatalf("added = %d, quer 0", got)
	}
	after := mustGet(t, s)
	if after.Status != before.Status || after.Cwd != before.Cwd ||
		after.TranscriptPath != before.TranscriptPath || !after.LastEventAt.Equal(before.LastEventAt) {
		t.Errorf("sessão conhecida mudou: antes %+v, depois %+v", before.Session, after.Session)
	}
	select {
	case <-ch:
		t.Error("Bootstrap sem sessão nova não deveria notificar")
	default:
	}
}

func TestBootstrapErrosNaoSaoFatais(t *testing.T) {
	now := time.Now()
	s := New()
	if got := s.Bootstrap(filepath.Join(t.TempDir(), "nao-existe"), now, bootMaxAge); got != 0 {
		t.Errorf("diretório inexistente: added = %d, quer 0", got)
	}
	if got := s.Bootstrap("", now, bootMaxAge); got != 0 {
		t.Errorf("diretório vazio: added = %d, quer 0", got)
	}

	// Arquivo ilegível é pulado; os demais entram.
	dir := t.TempDir()
	writeTranscript(t, dir, "p/ok.jsonl", lineUser+"\n", now)
	bad := writeTranscript(t, dir, "p/ruim.jsonl", lineUser+"\n", now)
	if err := os.Chmod(bad, 0); err != nil {
		t.Fatal(err)
	}
	if f, err := os.Open(bad); err == nil {
		f.Close()
		t.Log("arquivo sem permissão ainda legível (root?); só confere que não há pânico")
		s.Bootstrap(dir, now, bootMaxAge)
		return
	}
	if got := s.Bootstrap(dir, now, bootMaxAge); got != 1 {
		t.Errorf("added = %d, quer 1 (só ok.jsonl)", got)
	}
	if _, ok := s.Get("ruim"); ok {
		t.Error("arquivo ilegível virou sessão")
	}
}

// TestBootstrapPrimeiroEventoReal confere que o primeiro evento real tira a
// sessão descoberta no boot de desconhecido.
func TestBootstrapPrimeiroEventoReal(t *testing.T) {
	tests := []struct {
		name string
		ev   ingest.Event
		want Status
	}{
		{"UserPromptSubmit", ev("UserPromptSubmit"), StatusAtivo},
		{"PreToolUse", tool("PreToolUse", "Bash"), StatusAtivo},
		{"PostToolUse", tool("PostToolUse", "Bash"), StatusAtivo},
		{"PostToolUseFailure", tool("PostToolUseFailure", "Bash"), StatusAtivo},
		{"PostToolBatch", ev("PostToolBatch"), StatusAtivo},
		{"Stop", ev("Stop"), StatusParado},
		{"StopFailure", ev("StopFailure"), StatusParado},
		{"PermissionRequest", tool("PermissionRequest", "Write"), StatusAguardando},
		{"SessionStart(resume)", withSource("resume"), StatusParado},
		{"SessionStart(startup)", withSource("startup"), StatusParado},
		{"SessionEnd", ev("SessionEnd"), StatusEncerrado},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			now := time.Now()
			writeTranscript(t, dir, "p/"+sid+".jsonl", lineUser+"\n", now.Add(-time.Minute))

			s := New()
			if got := s.Bootstrap(dir, now, bootMaxAge); got != 1 {
				t.Fatalf("added = %d, quer 1", got)
			}
			if d := mustGet(t, s); d.Status != StatusDesconhecido {
				t.Fatalf("status após boot = %q, quer desconhecido", d.Status)
			}
			if !s.Apply(tt.ev, now) {
				t.Error("changed = false")
			}
			d := mustGet(t, s)
			if d.Status != tt.want {
				t.Errorf("status = %q, quer %q", d.Status, tt.want)
			}
			if !d.LastEventAt.Equal(now) {
				t.Errorf("LastEventAt = %v, quer %v", d.LastEventAt, now)
			}
		})
	}
}

// TestDesconhecidoEventosNeutros garante que eventos que não são "reais"
// para o status mantêm a sessão em desconhecido.
func TestDesconhecidoEventosNeutros(t *testing.T) {
	for _, name := range []string{"PreCompact", "PostCompact", "Notification", "AlgumEventoNovo"} {
		t.Run(name, func(t *testing.T) {
			s := New()
			seed(s, StatusDesconhecido)
			s.Apply(ev(name), at(10))
			if d := mustGet(t, s); d.Status != StatusDesconhecido {
				t.Errorf("status = %q, quer desconhecido", d.Status)
			}
		})
	}
}

func TestDesconhecidoSessionStartCompactNaoMudaStatus(t *testing.T) {
	s := New()
	seed(s, StatusDesconhecido)
	e := ev("SessionStart")
	e.Source = "compact"
	s.Apply(e, at(10))
	if d := mustGet(t, s); d.Status != StatusDesconhecido {
		t.Errorf("status = %q, quer desconhecido", d.Status)
	}
}
