package store

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/devpolatto/claude-fleet/internal/ingest"
)

const sid = "abcdef0123456789"

var t0 = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

// at devolve t0 + n segundos.
func at(n int) time.Time { return t0.Add(time.Duration(n) * time.Second) }

func ev(name string) ingest.Event {
	return ingest.Event{Name: name, SessionID: sid, Cwd: "/home/u/proj", TranscriptPath: "/t/" + sid + ".jsonl"}
}

func tool(name, toolName string) ingest.Event {
	e := ev(name)
	e.ToolName = toolName
	return e
}

func agentEv(name, agentID string) ingest.Event {
	e := ev(name)
	e.AgentID = agentID
	e.AgentType = "general-purpose"
	return e
}

// seed cria a sessão sid diretamente no estado interno, com o status dado.
func seed(s *Store, st Status) {
	s.sessions[sid] = &Session{
		ID: sid, Cwd: "/home/u/proj", Project: "proj", TranscriptPath: "/t/" + sid + ".jsonl",
		Status: st, StartedAt: t0, LastEventAt: t0, Subagents: map[string]*Agent{},
	}
	if st == StatusEncerrado {
		e := t0
		s.sessions[sid].EndedAt = &e
	}
}

func mustGet(t *testing.T, s *Store) SessionDetail {
	t.Helper()
	d, ok := s.Get(sid)
	if !ok {
		t.Fatalf("sessão %s não encontrada", sid)
	}
	return d
}

// TestSessionTransitions cobre cada seta do diagrama de estados da sessão
// (a seta encerrado → [*] é do Prune, T8) e as regras complementares.
func TestSessionTransitions(t *testing.T) {
	const none Status = "" // sessão ainda não existe
	tests := []struct {
		name string
		from Status
		ev   ingest.Event
		want Status
	}{
		// [*] --> parado / criação implícita
		{"[*] → parado: SessionStart", none, ev("SessionStart"), StatusParado},
		{"[*] → ativo: PreToolUse cria sessão desconhecida e já ativa", none, tool("PreToolUse", "Bash"), StatusAtivo},
		{"[*] → parado: Stop de sessão desconhecida", none, ev("Stop"), StatusParado},
		{"[*] → desconhecido: PreCompact de sessão desconhecida", none, ev("PreCompact"), StatusDesconhecido},

		// desconhecido --> ativo: qualquer evento de atividade
		{"desconhecido → ativo: UserPromptSubmit", StatusDesconhecido, ev("UserPromptSubmit"), StatusAtivo},
		{"desconhecido → ativo: PreToolUse", StatusDesconhecido, tool("PreToolUse", "Bash"), StatusAtivo},
		{"desconhecido → ativo: PostToolUse", StatusDesconhecido, tool("PostToolUse", "Bash"), StatusAtivo},
		{"desconhecido → ativo: PostToolUseFailure", StatusDesconhecido, tool("PostToolUseFailure", "Bash"), StatusAtivo},
		{"desconhecido → ativo: PostToolBatch", StatusDesconhecido, ev("PostToolBatch"), StatusAtivo},

		// parado --> ativo
		{"parado → ativo: UserPromptSubmit", StatusParado, ev("UserPromptSubmit"), StatusAtivo},
		{"parado → ativo: PreToolUse", StatusParado, tool("PreToolUse", "Bash"), StatusAtivo},
		{"parado → ativo: PostToolUse", StatusParado, tool("PostToolUse", "Bash"), StatusAtivo},

		// ativo --> aguardando
		{"ativo → aguardando: PermissionRequest", StatusAtivo, tool("PermissionRequest", "Write"), StatusAguardando},
		{"ativo → aguardando: Notification", StatusAtivo, ev("Notification"), StatusAguardando},

		// aguardando --> ativo
		{"aguardando → ativo: PreToolUse", StatusAguardando, tool("PreToolUse", "Bash"), StatusAtivo},
		{"aguardando → ativo: PostToolUse", StatusAguardando, tool("PostToolUse", "Bash"), StatusAtivo},
		{"aguardando → ativo: PostToolUseFailure", StatusAguardando, tool("PostToolUseFailure", "Bash"), StatusAtivo},
		{"aguardando → ativo: PostToolBatch (permissão negada)", StatusAguardando, ev("PostToolBatch"), StatusAtivo},
		{"aguardando → ativo: UserPromptSubmit", StatusAguardando, ev("UserPromptSubmit"), StatusAtivo},

		// --> parado
		{"ativo → parado: Stop", StatusAtivo, ev("Stop"), StatusParado},
		{"aguardando → parado: Stop", StatusAguardando, ev("Stop"), StatusParado},

		// --> encerrado
		{"parado → encerrado: SessionEnd", StatusParado, ev("SessionEnd"), StatusEncerrado},
		{"ativo → encerrado: SessionEnd", StatusAtivo, ev("SessionEnd"), StatusEncerrado},
		{"aguardando → encerrado: SessionEnd", StatusAguardando, ev("SessionEnd"), StatusEncerrado},

		// Regras complementares
		{"Notification em parado não muda (aviso de ociosidade)", StatusParado, ev("Notification"), StatusParado},
		{"SessionStart(compact) em ativo não muda", StatusAtivo, withSource("compact"), StatusAtivo},
		{"SessionStart(resume) em parado não muda", StatusParado, withSource("resume"), StatusParado},
		{"SessionStart(startup) em aguardando não muda", StatusAguardando, withSource("startup"), StatusAguardando},
		{"PreCompact em ativo não muda", StatusAtivo, ev("PreCompact"), StatusAtivo},
		{"PostCompact em aguardando não muda", StatusAguardando, ev("PostCompact"), StatusAguardando},
		{"evento desconhecido em ativo não muda", StatusAtivo, ev("AlgumEventoNovo"), StatusAtivo},

		// Regra 9: depois do SessionEnd
		{"encerrado → ativo: UserPromptSubmit reabre", StatusEncerrado, ev("UserPromptSubmit"), StatusAtivo},
		{"encerrado → parado: SessionStart(resume) reabre", StatusEncerrado, withSource("resume"), StatusParado},
		{"encerrado: PostToolUse atrasado não reabre", StatusEncerrado, tool("PostToolUse", "Bash"), StatusEncerrado},
		{"encerrado: Stop não reabre", StatusEncerrado, ev("Stop"), StatusEncerrado},
		{"encerrado: PermissionRequest não reabre", StatusEncerrado, ev("PermissionRequest"), StatusEncerrado},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New()
			if tt.from != none {
				seed(s, tt.from)
			}
			changed := s.Apply(tt.ev, at(10))
			if !changed {
				t.Error("changed = false; LastEventAt deveria ter mudado")
			}
			d := mustGet(t, s)
			if d.Status != tt.want {
				t.Errorf("status = %q, quer %q", d.Status, tt.want)
			}
			if !d.LastEventAt.Equal(at(10)) {
				t.Errorf("LastEventAt = %v, quer %v", d.LastEventAt, at(10))
			}
			if len(d.Subagents) != 0 {
				t.Errorf("evento sem agent_id criou %d subagente(s)", len(d.Subagents))
			}
			if tt.want == StatusEncerrado && d.EndedAt == nil {
				t.Error("encerrado sem EndedAt")
			}
			if tt.want != StatusEncerrado && d.EndedAt != nil {
				t.Errorf("EndedAt = %v em status %q", *d.EndedAt, d.Status)
			}
		})
	}
}

func withSource(src string) ingest.Event {
	e := ev("SessionStart")
	e.Source = src
	return e
}

// TestAgentTransitions cobre as regras de subagente do design.
func TestAgentTransitions(t *testing.T) {
	const none Status = ""
	tests := []struct {
		name     string
		from     Status // status inicial do subagente "a1"; none = não existe
		ev       ingest.Event
		want     Status // none = não deve existir
		wantTool string
	}{
		{"[*] → ativo: SubagentStart", none, agentEv("SubagentStart", "a1"), StatusAtivo, ""},
		{"ativo → ativo: PreToolUse com agent_id", StatusAtivo, withTool(agentEv("PreToolUse", "a1"), "Bash"), StatusAtivo, "Bash"},
		{"ativo → aguardando: PermissionRequest com agent_id", StatusAtivo, agentEv("PermissionRequest", "a1"), StatusAguardando, ""},
		{"aguardando → ativo: PreToolUse", StatusAguardando, withTool(agentEv("PreToolUse", "a1"), "Read"), StatusAtivo, "Read"},
		{"aguardando → ativo: PostToolUse", StatusAguardando, withTool(agentEv("PostToolUse", "a1"), "Read"), StatusAtivo, "Read"},
		{"aguardando → ativo: PostToolUseFailure", StatusAguardando, withTool(agentEv("PostToolUseFailure", "a1"), "Bash"), StatusAtivo, "Bash"},
		{"aguardando → ativo: PostToolBatch", StatusAguardando, agentEv("PostToolBatch", "a1"), StatusAtivo, ""},
		{"ativo → concluido: SubagentStop", StatusAtivo, agentEv("SubagentStop", "a1"), StatusConcluido, ""},
		{"aguardando → concluido: SubagentStop", StatusAguardando, agentEv("SubagentStop", "a1"), StatusConcluido, ""},
		{"concluido → ativo: novo SubagentStart", StatusConcluido, agentEv("SubagentStart", "a1"), StatusAtivo, ""},
		{"concluido: PostToolUse atrasado não reabre", StatusConcluido, withTool(agentEv("PostToolUse", "a1"), "Bash"), StatusConcluido, ""},
		{"desconhecido: PreToolUse cria o subagente (SubagentStart perdido)", none, withTool(agentEv("PreToolUse", "a1"), "Bash"), StatusAtivo, "Bash"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New()
			seed(s, StatusParado)
			if tt.from != none {
				a := &Agent{ID: "a1", Type: "general-purpose", Status: tt.from, StartedAt: t0, LastEventAt: t0}
				if tt.from == StatusConcluido {
					e := t0
					a.EndedAt = &e
				}
				s.sessions[sid].Subagents["a1"] = a
			}
			if !s.Apply(tt.ev, at(10)) {
				t.Error("changed = false")
			}
			d := mustGet(t, s)
			if d.Status != StatusParado {
				t.Errorf("status da sessão mudou para %q; evento com agent_id não deve alterá-lo", d.Status)
			}
			if d.LastTool != "" {
				t.Errorf("LastTool da sessão = %q; evento com agent_id não deve alterá-lo", d.LastTool)
			}
			if !d.LastEventAt.Equal(at(10)) {
				t.Errorf("LastEventAt da sessão = %v, quer %v", d.LastEventAt, at(10))
			}
			a := d.Subagents["a1"]
			if tt.want == none {
				if a != nil {
					t.Fatalf("subagente criado: %+v", a)
				}
				return
			}
			if a == nil {
				t.Fatal("subagente não existe")
			}
			if a.Status != tt.want {
				t.Errorf("status do subagente = %q, quer %q", a.Status, tt.want)
			}
			if a.LastTool != tt.wantTool {
				t.Errorf("LastTool do subagente = %q, quer %q", a.LastTool, tt.wantTool)
			}
			if a.Type != "general-purpose" {
				t.Errorf("Type = %q", a.Type)
			}
			if (a.Status == StatusConcluido) != (a.EndedAt != nil) {
				t.Errorf("EndedAt = %v com status %q", a.EndedAt, a.Status)
			}
		})
	}
}

func withTool(e ingest.Event, name string) ingest.Event {
	e.ToolName = name
	return e
}

func TestSubagentStopGuardaTranscript(t *testing.T) {
	s := New()
	s.Apply(ev("SessionStart"), at(0))
	s.Apply(agentEv("SubagentStart", "a1"), at(1))
	stop := agentEv("SubagentStop", "a1")
	stop.AgentTranscriptPath = "/t/sub/agent-a1.jsonl"
	s.Apply(stop, at(5))
	a := mustGet(t, s).Subagents["a1"]
	if a.TranscriptPath != "/t/sub/agent-a1.jsonl" {
		t.Errorf("TranscriptPath = %q", a.TranscriptPath)
	}
	if a.EndedAt == nil || !a.EndedAt.Equal(at(5)) {
		t.Errorf("EndedAt = %v, quer %v", a.EndedAt, at(5))
	}
	if !a.StartedAt.Equal(at(1)) {
		t.Errorf("StartedAt = %v, quer %v", a.StartedAt, at(1))
	}
}

func TestSubagentStopDesconhecidoIgnorado(t *testing.T) {
	t.Run("sessão conhecida", func(t *testing.T) {
		s := New()
		s.Apply(ev("SessionStart"), at(0))
		phantom := agentEv("SubagentStop", "fantasma")
		phantom.AgentType = ""
		if s.Apply(phantom, at(5)) {
			t.Error("changed = true para evento ignorado")
		}
		d := mustGet(t, s)
		if len(d.Subagents) != 0 {
			t.Errorf("criou mini-tile: %v", d.Subagents)
		}
		if !d.LastEventAt.Equal(at(0)) {
			t.Errorf("LastEventAt mudou para %v", d.LastEventAt)
		}
	})
	t.Run("sessão desconhecida", func(t *testing.T) {
		s := New()
		if s.Apply(agentEv("SubagentStop", "fantasma"), at(5)) {
			t.Error("changed = true para evento ignorado")
		}
		if _, ok := s.Get(sid); ok {
			t.Error("SubagentStop ignorado criou a sessão")
		}
	})
}

func TestFerramentaAgentNaSessaoPai(t *testing.T) {
	s := New()
	s.Apply(ev("SessionStart"), at(0))
	s.Apply(ev("UserPromptSubmit"), at(1))
	s.Apply(agentEv("SubagentStart", "a1"), at(2))
	s.Apply(withTool(agentEv("PermissionRequest", "a1"), "Bash"), at(3))
	s.Apply(agentEv("SubagentStart", "a2"), at(3))
	s.Apply(agentEv("SubagentStop", "a2"), at(4))

	s.Apply(tool("PreToolUse", "Agent"), at(5))
	s.Apply(tool("PostToolUse", "Agent"), at(6))

	d := mustGet(t, s)
	if d.LastTool != "Agent" {
		t.Errorf("LastTool da sessão = %q, quer Agent", d.LastTool)
	}
	if d.Status != StatusAtivo {
		t.Errorf("status da sessão = %q", d.Status)
	}
	if got := d.Subagents["a1"].Status; got != StatusAguardando {
		t.Errorf("a1 = %q, quer aguardando (intocado)", got)
	}
	if got := d.Subagents["a2"].Status; got != StatusConcluido {
		t.Errorf("a2 = %q, quer concluido (intocado)", got)
	}
	for id, a := range d.Subagents {
		if a.LastTool != "" {
			t.Errorf("%s.LastTool = %q", id, a.LastTool)
		}
	}
}

// TestCompactacaoSpike reproduz a sequência real do spike T1.
func TestCompactacaoSpike(t *testing.T) {
	s := New()
	s.Apply(ev("SessionStart"), at(0))
	s.Apply(tool("PreToolUse", "Bash"), at(1))

	compact := []ingest.Event{
		ev("PreCompact"),
		func() ingest.Event { e := agentEv("SubagentStop", "compact-xyz"); e.AgentType = ""; return e }(),
		withSource("compact"),
		ev("PostCompact"),
	}
	for i, e := range compact {
		s.Apply(e, at(2+i))
	}
	d := mustGet(t, s)
	if d.Status != StatusAtivo {
		t.Errorf("status = %q, quer ativo", d.Status)
	}
	if len(d.Subagents) != 0 {
		t.Errorf("compactação criou subagente: %v", d.Subagents)
	}
	if d.LastTool != "Bash" {
		t.Errorf("LastTool = %q", d.LastTool)
	}
	if !d.LastEventAt.Equal(at(5)) {
		t.Errorf("LastEventAt = %v, quer %v", d.LastEventAt, at(5))
	}
}

// TestSubagenteEmSegundoPlano reproduz o spike: Agent em segundo plano, Stop
// do pai com subagente ativo e o "despertar" da sessão.
func TestSubagenteEmSegundoPlano(t *testing.T) {
	s := New()
	steps := []ingest.Event{
		ev("SessionStart"),
		ev("UserPromptSubmit"),
		tool("PreToolUse", "Agent"),
		agentEv("SubagentStart", "a1"),
		tool("PostToolUse", "Agent"),
		ev("Stop"),
		withTool(agentEv("PreToolUse", "a1"), "Bash"),
	}
	for i, e := range steps {
		s.Apply(e, at(i))
	}
	d := mustGet(t, s)
	if d.Status != StatusParado || d.Subagents["a1"].Status != StatusAtivo {
		t.Fatalf("pai = %q, a1 = %q; quer parado com subagente ativo", d.Status, d.Subagents["a1"].Status)
	}
	s.Apply(agentEv("SubagentStop", "a1"), at(10))
	s.Apply(ev("UserPromptSubmit"), at(11))
	if d := mustGet(t, s); d.Status != StatusAtivo || d.Subagents["a1"].Status != StatusConcluido {
		t.Errorf("pai = %q, a1 = %q; quer ativo e concluido", d.Status, d.Subagents["a1"].Status)
	}
	s.Apply(ev("Stop"), at(12))
	if d := mustGet(t, s); d.Status != StatusParado {
		t.Errorf("pai = %q, quer parado", d.Status)
	}
}

// TestPermissaoNegada: PermissionRequest seguido só de PostToolBatch.
func TestPermissaoNegada(t *testing.T) {
	s := New()
	for i, e := range []ingest.Event{
		ev("SessionStart"), ev("UserPromptSubmit"), tool("PermissionRequest", "Write"),
	} {
		s.Apply(e, at(i))
	}
	if d := mustGet(t, s); d.Status != StatusAguardando {
		t.Fatalf("status = %q, quer aguardando", d.Status)
	}
	s.Apply(ev("PostToolBatch"), at(5))
	if d := mustGet(t, s); d.Status != StatusAtivo {
		t.Errorf("status = %q, quer ativo", d.Status)
	}
}

func TestCamposDaSessao(t *testing.T) {
	s := New()
	start := ev("SessionStart")
	start.Cwd = "/home/u/code/claude-fleet"
	s.Apply(start, at(0))
	s.Apply(tool("PreToolUse", "Bash"), at(1)) // Cwd /home/u/proj no helper
	end := ev("SessionEnd")
	end.Cwd = ""
	end.TranscriptPath = ""
	s.Apply(end, at(2))

	d := mustGet(t, s)
	if d.Project != "proj" || d.Cwd != "/home/u/proj" {
		t.Errorf("Project = %q, Cwd = %q", d.Project, d.Cwd)
	}
	if d.TranscriptPath != "/t/"+sid+".jsonl" {
		t.Errorf("TranscriptPath = %q (evento sem transcript não deve apagar)", d.TranscriptPath)
	}
	if !d.StartedAt.Equal(at(0)) || d.EndedAt == nil || !d.EndedAt.Equal(at(2)) {
		t.Errorf("StartedAt = %v, EndedAt = %v", d.StartedAt, d.EndedAt)
	}

	v := s.Snapshot()
	if len(v) != 1 || v[0].ShortID != "abcdef01" || v[0].Project != "proj" || v[0].LastTool != "Bash" {
		t.Errorf("Snapshot = %+v", v)
	}

	// Reabrir limpa EndedAt.
	s.Apply(ev("UserPromptSubmit"), at(3))
	if d := mustGet(t, s); d.EndedAt != nil || d.Status != StatusAtivo {
		t.Errorf("depois de reabrir: status = %q, EndedAt = %v", d.Status, d.EndedAt)
	}
}

func TestChanged(t *testing.T) {
	s := New()
	if !s.Apply(ev("SessionStart"), at(0)) {
		t.Error("criação: changed = false")
	}
	if s.Apply(ev("SessionStart"), at(0)) {
		t.Error("mesmo evento no mesmo instante: changed = true")
	}
	if s.Apply(ev("Stop"), at(0)) {
		t.Error("Stop em parado no mesmo instante: changed = true")
	}
	if !s.Apply(ev("Stop"), at(1)) {
		t.Error("LastEventAt avançou: changed = false")
	}
	if s.Apply(ingest.Event{Name: "Stop"}, at(2)) {
		t.Error("evento sem session_id: changed = true")
	}
}

func TestSnapshotOrdenado(t *testing.T) {
	s := New()
	for i, id := range []string{"c", "a", "b"} {
		e := ev("SessionStart")
		e.SessionID = id
		s.Apply(e, at(i))
	}
	e := ev("SessionStart")
	e.SessionID = "0"
	s.Apply(e, at(1)) // empata com "a"; desempata por ID
	var got []string
	for _, v := range s.Snapshot() {
		got = append(got, v.ID)
	}
	if fmt.Sprint(got) != "[c 0 a b]" {
		t.Errorf("ordem = %v", got)
	}
}

func TestCopiasProfundas(t *testing.T) {
	s := New()
	s.Apply(ev("SessionStart"), at(0))
	s.Apply(agentEv("SubagentStart", "a1"), at(1))
	s.Apply(agentEv("SubagentStop", "a1"), at(2))
	s.Apply(ev("SessionEnd"), at(3))
	s.sessions[sid].Models = map[string]Usage{"m": {CostUSD: 1}}
	s.sessions[sid].Subagents["a1"].Models = map[string]Usage{"m": {CostUSD: 2}}

	d := mustGet(t, s)
	d.Status = StatusAtivo
	*d.EndedAt = at(99)
	d.Models["m"] = Usage{CostUSD: 50}
	d.Models["x"] = Usage{}
	d.Subagents["a1"].Status = StatusAtivo
	*d.Subagents["a1"].EndedAt = at(99)
	d.Subagents["a1"].Models["m"] = Usage{CostUSD: 50}
	d.Subagents["a2"] = &Agent{}

	v := s.Snapshot()
	*v[0].EndedAt = at(99)
	*v[0].Subagents[0].EndedAt = at(99)
	v[0].Subagents[0].Status = StatusAtivo

	in := s.sessions[sid]
	if in.Status != StatusEncerrado || !in.EndedAt.Equal(at(3)) {
		t.Errorf("sessão interna alterada: %q, %v", in.Status, in.EndedAt)
	}
	if len(in.Models) != 1 || in.Models["m"].CostUSD != 1 {
		t.Errorf("Models interno alterado: %v", in.Models)
	}
	if len(in.Subagents) != 1 {
		t.Errorf("mapa de subagentes interno alterado: %v", in.Subagents)
	}
	a := in.Subagents["a1"]
	if a.Status != StatusConcluido || !a.EndedAt.Equal(at(2)) || a.Models["m"].CostUSD != 2 {
		t.Errorf("subagente interno alterado: %+v", a)
	}
}

func TestSubscribe(t *testing.T) {
	t.Run("recebe sinal após mudança", func(t *testing.T) {
		s := New()
		ch := s.Subscribe()
		s.Apply(ev("SessionStart"), at(0))
		select {
		case <-ch:
		case <-time.After(time.Second):
			t.Fatal("nenhum sinal após mudança")
		}
	})
	t.Run("sem sinal quando nada muda", func(t *testing.T) {
		s := New()
		s.Apply(ev("SessionStart"), at(0))
		ch := s.Subscribe()
		s.Apply(agentEv("SubagentStop", "fantasma"), at(1))
		select {
		case <-ch:
			t.Fatal("sinal para evento ignorado")
		default:
		}
	})
	t.Run("assinante que nunca lê não trava Apply e sinais se fundem", func(t *testing.T) {
		s := New()
		_ = s.Subscribe() // nunca lido
		ch := s.Subscribe()
		done := make(chan struct{})
		go func() {
			for i := range 1000 {
				s.Apply(ev("UserPromptSubmit"), at(i))
			}
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("Apply travou com assinante lento")
		}
		if len(ch) != 1 {
			t.Errorf("buffer = %d, quer 1 sinal coalescido", len(ch))
		}
	})
}

// TestConcorrencia deve rodar com -race.
func TestConcorrencia(t *testing.T) {
	s := New()
	ch := s.Subscribe()
	var wg sync.WaitGroup
	stop := make(chan struct{})

	for w := range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			names := []string{"SessionStart", "UserPromptSubmit", "PreToolUse", "PermissionRequest", "PostToolBatch", "Stop", "SessionEnd"}
			for i := range 300 {
				e := ev(names[i%len(names)])
				e.SessionID = fmt.Sprintf("s%d", i%5)
				e.ToolName = "Bash"
				if i%3 == 0 {
					e.AgentID = fmt.Sprintf("a%d", w)
					if i%2 == 0 {
						e.Name = "SubagentStart"
					}
				}
				s.Apply(e, at(i))
			}
		}()
	}
	var readers sync.WaitGroup
	for range 4 {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				case <-ch:
				default:
				}
				for _, v := range s.Snapshot() {
					if d, ok := s.Get(v.ID); ok {
						for _, a := range d.Subagents {
							a.Status = StatusAtivo // escrever na cópia não pode correr com o Store
						}
						d.Models = nil
					}
				}
			}
		}()
	}
	wg.Wait()
	close(stop)
	readers.Wait()
	if n := len(s.Snapshot()); n != 5 {
		t.Errorf("sessões = %d, quer 5", n)
	}
}

func TestCwdDeSubagenteNaoRenomeiaSessao(t *testing.T) {
	s := New()
	e := ev("SessionStart")
	e.Cwd = "/home/x/projeto"
	s.Apply(e, t0)

	a := agentEv("PreToolUse", "a1")
	a.Cwd = "/home/x/projeto/.claude/worktrees/outro"
	s.Apply(a, t0.Add(time.Second))
	if d := mustGet(t, s); d.Project != "projeto" || d.Cwd != "/home/x/projeto" {
		t.Errorf("Project = %q, Cwd = %q; o subagente não deveria renomear a sessão", d.Project, d.Cwd)
	}

	// Sessão criada por evento de subagente usa o cwd dele até chegar um da sessão.
	s2 := New()
	s2.Apply(a, t0)
	if d := mustGet(t, s2); d.Project != "outro" {
		t.Errorf("sem cwd próprio, Project = %q, queria outro", d.Project)
	}
}
