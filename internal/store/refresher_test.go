package store

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/devpolatto/claude-fleet/internal/ingest"
	"github.com/devpolatto/claude-fleet/internal/pricing"
)

// Linhas sintéticas de transcript. Os valores de custo esperados abaixo
// foram calculados à mão com a tabela de preços (USD por milhão):
//
//	claude-opus-5-5:  input 4, output 20, cache read 0,20
//	claude-haiku-4-5: input 1, output 5,  cache read 0,10
//	cache 5m = 1,25 × input; cache 1h = 2 × input; fast = 2×
const (
	// Principal: msg_1 aparece duas vezes (só a última conta) e msg_2 é fast.
	mainLines = `{"type":"user","timestamp":"2026-10-15T12:00:00Z","message":{"role":"user","content":"oi"}}
{"type":"assistant","timestamp":"2026-10-15T12:00:01Z","message":{"id":"msg_1","model":"claude-opus-5-5","usage":{"input_tokens":1,"output_tokens":1}}}
{"type":"assistant","timestamp":"2026-10-15T12:00:02Z","message":{"id":"msg_1","model":"claude-opus-5-5","usage":{"input_tokens":100,"cache_read_input_tokens":10000,"output_tokens":500,"cache_creation":{"ephemeral_5m_input_tokens":1000,"ephemeral_1h_input_tokens":0}}}}
{"type":"assistant","timestamp":"2026-10-15T12:00:03Z","message":{"id":"msg_2","model":"claude-opus-5-5","usage":{"input_tokens":10,"output_tokens":100,"speed":"fast"}}}
`
	// Subagente a1 (haiku): 200·1 + 2000·1·2 + 300·5 = 5700 → 0,0057.
	agent1Lines = `{"type":"assistant","timestamp":"2026-10-15T12:01:00Z","message":{"id":"msg_a1","model":"claude-haiku-4-5","usage":{"input_tokens":200,"output_tokens":300,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":2000}}}}
`
	// Subagente a2 (opus 5.5): 50·4 + 1000·0,2 + 1000·20 = 20400 → 0,0204.
	agent2Lines = `{"type":"assistant","timestamp":"2026-10-15T12:02:00Z","message":{"id":"msg_a2","model":"claude-opus-5-5","usage":{"input_tokens":50,"cache_read_input_tokens":1000,"output_tokens":1000}}}
{"type":"assistant","timestamp":"2026-10-15T12:02:30Z","message":{"id":"msg_s","model":"<synthetic>","usage":{"input_tokens":0,"output_tokens":0}}}
`
)

// Principal: msg_1 = 100·4 + 1000·4·1,25 + 10000·0,2 + 500·20 = 17400 → 0,0174;
// msg_2 fast = (10·4 + 100·20)·2 = 4080 → 0,00408. Soma 0,02148.
const (
	mainCost   = 0.02148
	agent1Cost = 0.0057
	agent2Cost = 0.0204
)

var (
	mainTokens   = pricing.Tokens{Input: 110, Cache5m: 1000, CacheRead: 10000, Output: 600}
	agent1Tokens = pricing.Tokens{Input: 200, Cache1h: 2000, Output: 300}
	agent2Tokens = pricing.Tokens{Input: 50, CacheRead: 1000, Output: 1000}
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// fixture monta <dir>/<sid>.jsonl com dois subagentes e devolve o caminho
// do transcript principal.
func fixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mainPath := filepath.Join(dir, sid+".jsonl")
	sub := filepath.Join(dir, sid, "subagents")
	writeFile(t, mainPath, mainLines)
	writeFile(t, filepath.Join(sub, "agent-a1.jsonl"), agent1Lines)
	writeFile(t, filepath.Join(sub, "agent-a1.meta.json"), `{"agentType":"Explore","description":"procura arquivos"}`)
	writeFile(t, filepath.Join(sub, "agent-a2.jsonl"), agent2Lines)
	writeFile(t, filepath.Join(sub, "agent-a2.meta.json"), `{"agentType":"general-purpose","description":"escreve testes"}`)
	return mainPath
}

// startSession cria a sessão sid no Store apontando para mainPath.
func startSession(s *Store, mainPath string) {
	s.Apply(ingest.Event{Name: evSessionStart, SessionID: sid, Cwd: "/home/u/proj", TranscriptPath: mainPath}, t0)
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-12 }

func checkUsage(t *testing.T, what string, got Usage, tok pricing.Tokens, cost float64) {
	t.Helper()
	if got.Tokens != tok {
		t.Errorf("%s: tokens = %+v, quero %+v", what, got.Tokens, tok)
	}
	if !near(got.CostUSD, cost) {
		t.Errorf("%s: custo = %.10f, quero %.10f", what, got.CostUSD, cost)
	}
	if !got.CostKnown {
		t.Errorf("%s: CostKnown = false, quero true", what)
	}
}

// drain esvazia o canal de notificação.
func drain(ch <-chan struct{}) {
	select {
	case <-ch:
	default:
	}
}

func notified(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

func TestRefresherTotalPrincipalMaisSubagentes(t *testing.T) {
	s := New()
	mainPath := fixture(t)
	startSession(s, mainPath)
	// a1 é conhecido por evento (descrição vazia); a2 só existe no disco.
	e := agentEv(evSubagentStart, "a1")
	e.TranscriptPath = mainPath
	s.Apply(e, at(1))

	NewRefresher(s).RefreshOnce()
	d := mustGet(t, s)

	checkUsage(t, "Main", d.Main, mainTokens, mainCost)
	checkUsage(t, "a1", d.Subagents["a1"].Usage, agent1Tokens, agent1Cost)
	checkUsage(t, "a2", d.Subagents["a2"].Usage, agent2Tokens, agent2Cost)

	totalTok := pricing.Tokens{Input: 360, Cache5m: 1000, Cache1h: 2000, CacheRead: 11000, Output: 1900}
	checkUsage(t, "Total", d.Total, totalTok, 0.04758)
	if d.TranscriptMissing {
		t.Error("TranscriptMissing = true com transcript presente")
	}

	// Models agrega por nome: fast e padrão do opus 5.5 somados.
	if len(d.Models) != 1 {
		t.Fatalf("Models = %v, quero só claude-opus-5-5", d.Models)
	}
	checkUsage(t, "Models[opus]", d.Models["claude-opus-5-5"], mainTokens, mainCost)
	// <synthetic> com tokens zerados não entra nem torna o custo n/d.
	if _, ok := d.Subagents["a2"].Models["<synthetic>"]; ok {
		t.Error("<synthetic> zerado apareceu em Models de a2")
	}

	a1 := d.Subagents["a1"]
	if a1.Status != StatusAtivo {
		t.Errorf("a1.Status = %s, quero ativo (refresher não muda status)", a1.Status)
	}
	if a1.Description != "procura arquivos" {
		t.Errorf("a1.Description = %q", a1.Description)
	}
	if a1.Type != "general-purpose" {
		t.Errorf("a1.Type = %q, quero o do evento", a1.Type)
	}
}

func TestRefresherSubagenteSoNoDisco(t *testing.T) {
	s := New()
	startSession(s, fixture(t))
	NewRefresher(s).RefreshOnce()

	a2 := mustGet(t, s).Subagents["a2"]
	if a2 == nil {
		t.Fatal("subagente a2, só no disco, não apareceu")
	}
	if a2.Status != StatusConcluido {
		t.Errorf("a2.Status = %s, quero concluido", a2.Status)
	}
	if a2.Type != "general-purpose" || a2.Description != "escreve testes" {
		t.Errorf("a2 Type/Description = %q/%q", a2.Type, a2.Description)
	}
	if want := time.Date(2026, 10, 15, 12, 2, 0, 0, time.UTC); !a2.StartedAt.Equal(want) {
		t.Errorf("a2.StartedAt = %v, quero %v", a2.StartedAt, want)
	}
	// Aparece também no resumo do tile.
	var found bool
	for _, a := range s.Snapshot()[0].Subagents {
		found = found || (a.ID == "a2" && a.Status == StatusConcluido)
	}
	if !found {
		t.Error("a2 ausente do Snapshot")
	}
}

func TestRefresherTranscriptAusente(t *testing.T) {
	s := New()
	mainPath := fixture(t)
	startSession(s, mainPath)
	r := NewRefresher(s)
	r.RefreshOnce()

	if err := os.Remove(mainPath); err != nil {
		t.Fatal(err)
	}
	r.RefreshOnce()
	d := mustGet(t, s)
	if !d.TranscriptMissing {
		t.Fatal("TranscriptMissing = false com transcript apagado")
	}
	checkUsage(t, "Main mantido", d.Main, mainTokens, mainCost)

	// Sessão que nunca teve transcript: sem pânico, totais zerados.
	s2 := New()
	startSession(s2, filepath.Join(t.TempDir(), sid+".jsonl"))
	NewRefresher(s2).RefreshOnce()
	d2 := mustGet(t, s2)
	if !d2.TranscriptMissing || d2.Main != (Usage{}) || len(d2.Subagents) != 0 {
		t.Errorf("sessão sem transcript: missing=%v Main=%+v subagentes=%d", d2.TranscriptMissing, d2.Main, len(d2.Subagents))
	}

	// O transcript volta: a flag cai.
	writeFile(t, mainPath, mainLines)
	r.RefreshOnce()
	if mustGet(t, s).TranscriptMissing {
		t.Error("TranscriptMissing continua true depois do transcript voltar")
	}
}

func TestRefresherSemMudancaNaoNotifica(t *testing.T) {
	s := New()
	mainPath := fixture(t)
	startSession(s, mainPath)
	ch := s.Subscribe()
	drain(ch)

	r := NewRefresher(s)
	r.RefreshOnce()
	if !notified(ch) {
		t.Fatal("primeira passada não notificou")
	}
	for i := 0; i < 3; i++ {
		r.RefreshOnce()
		if notified(ch) {
			t.Fatalf("passada %d sem mudança no disco notificou", i+2)
		}
	}

	// Leitura incremental: só a linha nova é somada.
	f, err := os.OpenFile(mainPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString(`{"type":"assistant","timestamp":"2026-10-15T12:05:00Z","message":{"id":"msg_3","model":"claude-opus-5-5","usage":{"output_tokens":1000}}}` + "\n")
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	r.RefreshOnce()
	if !notified(ch) {
		t.Fatal("linha nova no transcript não notificou")
	}
	want := mainTokens
	want.Output += 1000
	checkUsage(t, "Main após append", mustGet(t, s).Main, want, mainCost+0.02)
}

func TestRefresherSessaoEncerrada(t *testing.T) {
	s := New()
	mainPath := fixture(t)
	startSession(s, mainPath)
	r := NewRefresher(s)
	r.RefreshOnce()

	// Linha nova escrita pouco antes do fim: a última leitura a pega.
	appendLine := func(id string) {
		t.Helper()
		f, err := os.OpenFile(mainPath, os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if _, err := f.WriteString(`{"type":"assistant","message":{"id":"` + id + `","model":"claude-opus-5-5","usage":{"output_tokens":1000}}}` + "\n"); err != nil {
			t.Fatal(err)
		}
	}
	appendLine("msg_fim")
	s.Apply(ingest.Event{Name: evSessionEnd, SessionID: sid}, at(10))

	r.RefreshOnce()
	if got := mustGet(t, s).Main.Tokens.Output; got != mainTokens.Output+1000 {
		t.Fatalf("última leitura: Output = %d, quero %d", got, mainTokens.Output+1000)
	}
	if len(r.files) != 0 {
		t.Errorf("cache com %d leitores depois da última leitura", len(r.files))
	}

	// Depois da última leitura a sessão não é mais lida.
	appendLine("msg_depois")
	ch := s.Subscribe()
	r.RefreshOnce()
	if notified(ch) {
		t.Error("sessão encerrada foi lida de novo")
	}
	if got := mustGet(t, s).Main.Tokens.Output; got != mainTokens.Output+1000 {
		t.Errorf("Output mudou depois do fim: %d", got)
	}

	// Reaberta (--resume), volta a ser lida.
	s.Apply(ingest.Event{Name: evUserPromptSubmit, SessionID: sid}, at(20))
	r.RefreshOnce()
	if got := mustGet(t, s).Main.Tokens.Output; got != mainTokens.Output+2000 {
		t.Errorf("sessão reaberta: Output = %d, quero %d", got, mainTokens.Output+2000)
	}
}

func TestRefresherLimpaCacheDeSessaoRemovida(t *testing.T) {
	s := New()
	startSession(s, fixture(t))
	r := NewRefresher(s)
	r.RefreshOnce()
	if len(r.files) != 3 {
		t.Fatalf("cache com %d leitores, quero 3 (principal + 2 subagentes)", len(r.files))
	}

	// Simula o Prune removendo a sessão.
	s.mu.Lock()
	delete(s.sessions, sid)
	s.mu.Unlock()

	r.RefreshOnce()
	if len(r.files) != 0 || len(r.paths) != 0 {
		t.Errorf("cache não foi limpo: files=%d paths=%d", len(r.files), len(r.paths))
	}
}

func TestSetUsageSessaoDesconhecida(t *testing.T) {
	if New().SetUsage("nada", SessionUsage{Main: Usage{CostKnown: true}}) {
		t.Error("SetUsage em sessão desconhecida devolveu true")
	}
}

func TestRefresherModeloSemPreco(t *testing.T) {
	s := New()
	dir := t.TempDir()
	mainPath := filepath.Join(dir, sid+".jsonl")
	writeFile(t, mainPath, `{"type":"assistant","message":{"id":"m","model":"modelo-novo","usage":{"input_tokens":10}}}
`+`{"type":"assistant","message":{"id":"n","model":"claude-haiku-4-5","usage":{"output_tokens":1000000}}}
`)
	startSession(s, mainPath)
	NewRefresher(s).RefreshOnce()

	d := mustGet(t, s)
	if d.Main.CostKnown {
		t.Error("CostKnown = true com modelo sem preço")
	}
	if d.Main.Tokens.Input != 10 || !near(d.Main.CostUSD, 5) {
		t.Errorf("Main = %+v; quero tokens contados e custo só da parte conhecida (5)", d.Main)
	}
	if d.Models["modelo-novo"].CostKnown || !d.Models["claude-haiku-4-5"].CostKnown {
		t.Errorf("Models = %+v", d.Models)
	}
}

func TestRefresherRunParaNoCancelamento(t *testing.T) {
	s := New()
	startSession(s, fixture(t))
	r := NewRefresher(s)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		r.Run(ctx, time.Millisecond)
		close(done)
	}()

	deadline := time.After(2 * time.Second)
	for mustGet(t, s).Main.Tokens != mainTokens {
		select {
		case <-deadline:
			t.Fatal("Run não fez nenhuma passada")
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run não parou depois do cancelamento")
	}
}

func TestTotalComSubagenteSemLeituraTemCustoConhecido(t *testing.T) {
	s := New()
	s.Apply(ev("SessionStart"), t0)
	s.Apply(agentEv("SubagentStart", "a1"), t0.Add(time.Second))
	if d := mustGet(t, s); !d.Total.CostKnown || d.Total.CostUSD != 0 {
		t.Errorf("Total = %+v, queria custo conhecido de US$ 0", d.Total)
	}
}

func TestRefresherContaLinhasIlegiveis(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, sid+".jsonl")
	conteudo := `{"type":"assistant","timestamp":"2026-10-15T12:00:00Z","message":{"id":"m1","model":"claude-opus-5-5","usage":{"input_tokens":1,"output_tokens":1}}}
isto não é json
`
	if err := os.WriteFile(mainPath, []byte(conteudo), 0o600); err != nil {
		t.Fatal(err)
	}
	s := New()
	s.Apply(ingest.Event{Name: evSessionStart, SessionID: sid, TranscriptPath: mainPath}, t0)
	NewRefresher(s).RefreshOnce()
	if d := mustGet(t, s); d.ParseErrors != 1 {
		t.Errorf("ParseErrors = %d, queria 1", d.ParseErrors)
	}
}

// Recusar permissão no modo interativo não dispara hook nenhum; o fim de
// turno no transcript tira a sessão de aguardando.
func TestFimDeTurnoSemHookDeixaParado(t *testing.T) {
	tests := []struct {
		name   string
		fimEm  time.Duration // fim do turno relativo ao PermissionRequest
		inicio Status
		want   Status
	}{
		{"recusa depois do PermissionRequest", 300 * time.Millisecond, StatusAguardando, StatusParado},
		{"turno anterior ao PermissionRequest", -10 * time.Second, StatusAguardando, StatusAguardando},
		{"Esc com a sessão ativa", time.Second, StatusAtivo, StatusParado},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			mainPath := filepath.Join(dir, sid+".jsonl")
			fim := t0.Add(tt.fimEm).UTC().Format(time.RFC3339Nano)
			if err := os.WriteFile(mainPath, []byte(`{"type":"system","subtype":"turn_duration","timestamp":"`+fim+`"}`+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			s := New()
			s.Apply(ingest.Event{Name: evSessionStart, SessionID: sid, TranscriptPath: mainPath}, t0.Add(-time.Minute))
			name := "PermissionRequest"
			if tt.inicio == StatusAtivo {
				name = "PreToolUse"
			}
			s.Apply(ingest.Event{Name: name, SessionID: sid, ToolName: "Write"}, t0)
			if d := mustGet(t, s); d.Status != tt.inicio {
				t.Fatalf("status inicial = %q, queria %q", d.Status, tt.inicio)
			}
			NewRefresher(s).RefreshOnce()
			if d := mustGet(t, s); d.Status != tt.want {
				t.Errorf("status = %q, queria %q", d.Status, tt.want)
			}
		})
	}
}

// Matar os agentes em background (Esc) não dispara SubagentStop; a linha
// system/agents_killed no transcript principal conclui quem estava rodando.
func TestAgentesMortosSemHookFicamConcluidos(t *testing.T) {
	tests := []struct {
		name    string
		mortoEm time.Duration // agents_killed relativo ao último evento do subagente
		inicio  string        // evento que deixa o subagente no estado inicial
		want    Status
	}{
		{"ativo morto depois do último evento", 20 * time.Second, "PreToolUse", StatusConcluido},
		{"aguardando morto depois do último evento", 20 * time.Second, "PermissionRequest", StatusConcluido},
		{"subagente iniciado depois da morte", -10 * time.Second, "SubagentStart", StatusAtivo},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			mainPath := filepath.Join(dir, sid+".jsonl")
			morto := t0.Add(tt.mortoEm)
			linha := `{"type":"system","subtype":"agents_killed","timestamp":"` + morto.UTC().Format(time.RFC3339Nano) + `"}`
			if err := os.WriteFile(mainPath, []byte(linha+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			// Agente morto pelo Esc é real: tem transcript em subagents/.
			sub := filepath.Join(dir, sid, "subagents")
			if err := os.MkdirAll(sub, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(sub, "agent-a1.jsonl"), nil, 0o600); err != nil {
				t.Fatal(err)
			}
			s := New()
			s.Apply(ingest.Event{Name: evSessionStart, SessionID: sid, TranscriptPath: mainPath}, t0.Add(-time.Minute))
			s.Apply(ingest.Event{Name: evSubagentStart, SessionID: sid, AgentID: "a1", AgentType: "Explore"}, t0.Add(-time.Minute))
			s.Apply(ingest.Event{Name: tt.inicio, SessionID: sid, AgentID: "a1", ToolName: "Bash"}, t0)
			NewRefresher(s).RefreshOnce()
			ag := mustGet(t, s).Subagents["a1"]
			if ag.Status != tt.want {
				t.Fatalf("status = %q, queria %q", ag.Status, tt.want)
			}
			if tt.want == StatusConcluido && (ag.EndedAt == nil || !ag.EndedAt.Equal(morto)) {
				t.Errorf("EndedAt = %v, queria %v", ag.EndedAt, morto)
			}
		})
	}
}

// Atividade com agent_id desconhecido cria o mini-tile, mas há agentes
// internos do Claude Code sem transcript que nunca mandam SubagentStop (#30).
func TestSubagenteSemTranscriptQuietoFicaConcluido(t *testing.T) {
	const (
		semArquivo  = ""
		arquivoOK   = "ok"
		ilegivel    = "symlink quebrado" // aparece no ReadDir, falha no Refresh
		dirIlegivel = "subagents sem permissão"
	)
	tests := []struct {
		name    string
		quieto  time.Duration // da última atividade até a leitura
		evento  string
		arquivo string // estado de agent-a1.jsonl em subagents/
		semMain bool   // transcript principal ausente
		morto   bool   // agents_killed 20 s depois da última atividade
		want    Status
		fimEm   time.Duration // EndedAt esperado, relativo à última atividade
	}{
		{"sem transcript e quieto", 3 * time.Minute, "PostToolUse", semArquivo, false, false, StatusConcluido, 0},
		{"sem transcript aguardando", 3 * time.Minute, "PermissionRequest", semArquivo, false, false, StatusConcluido, 0},
		{"sem transcript com evento recente", 30 * time.Second, "PostToolUse", semArquivo, false, false, StatusAtivo, 0},
		{"sem transcript morto pelo Esc", 3 * time.Minute, "PostToolUse", semArquivo, false, true, StatusConcluido, 20 * time.Second},
		{"real em ferramenta longa", 10 * time.Minute, "PreToolUse", arquivoOK, false, false, StatusAtivo, 0},
		{"real esperando permissão", 10 * time.Minute, "PermissionRequest", arquivoOK, false, false, StatusAguardando, 0},
		{"transcript do subagente ilegível", 10 * time.Minute, "PreToolUse", ilegivel, false, false, StatusAtivo, 0},
		{"diretório subagents ilegível", 10 * time.Minute, "PreToolUse", dirIlegivel, false, false, StatusAtivo, 0},
		{"transcript principal ausente", 3 * time.Minute, "PostToolUse", semArquivo, true, false, StatusAtivo, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			mainPath := filepath.Join(dir, sid+".jsonl")
			if !tt.semMain {
				var linha []byte
				if tt.morto {
					linha = []byte(`{"type":"system","subtype":"agents_killed","timestamp":"` + t0.Add(20*time.Second).UTC().Format(time.RFC3339Nano) + `"}` + "\n")
				}
				if err := os.WriteFile(mainPath, linha, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			sub := filepath.Join(dir, sid, "subagents")
			agente := filepath.Join(sub, "agent-a1.jsonl")
			if tt.arquivo != semArquivo {
				if err := os.MkdirAll(sub, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			switch tt.arquivo {
			case arquivoOK:
				if err := os.WriteFile(agente, nil, 0o600); err != nil {
					t.Fatal(err)
				}
			case ilegivel:
				if err := os.Symlink(filepath.Join(dir, "nao-existe"), agente); err != nil {
					t.Fatal(err)
				}
			case dirIlegivel:
				if os.Geteuid() == 0 {
					t.Skip("root lê o diretório sem permissão")
				}
				if err := os.Chmod(sub, 0); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { os.Chmod(sub, 0o700) })
			}
			s := New()
			s.Apply(ingest.Event{Name: evSessionStart, SessionID: sid, TranscriptPath: mainPath}, t0.Add(-time.Hour))
			s.Apply(ingest.Event{Name: tt.evento, SessionID: sid, AgentID: "a1", ToolName: "Bash"}, t0)
			r := NewRefresher(s)
			r.now = func() time.Time { return t0.Add(tt.quieto) }
			r.RefreshOnce()
			ag := mustGet(t, s).Subagents["a1"]
			if ag.Status != tt.want {
				t.Fatalf("status = %q, queria %q", ag.Status, tt.want)
			}
			if fim := t0.Add(tt.fimEm); tt.want == StatusConcluido && (ag.EndedAt == nil || !ag.EndedAt.Equal(fim)) {
				t.Errorf("EndedAt = %v, queria %v", ag.EndedAt, fim)
			}
		})
	}
}

func TestRefresherUsaNomeDaSessao(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, sid+".jsonl")
	if err := os.WriteFile(mainPath, []byte(`{"type":"custom-title","customTitle":"  toni-voz\u0007 ","sessionId":"x"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := New()
	s.Apply(ingest.Event{Name: evSessionStart, SessionID: sid, Cwd: "/home/polatto", TranscriptPath: mainPath}, t0)
	NewRefresher(s).RefreshOnce()
	views := s.Snapshot()
	if len(views) != 1 || views[0].Name != "toni-voz" || views[0].Project != "polatto" {
		t.Fatalf("Snapshot = %+v, queria Name=toni-voz e Project=polatto", views)
	}
}

func TestSanitizeName(t *testing.T) {
	if got := sanitizeName(strings.Repeat("a", 70)); len([]rune(got)) != maxNameRunes+1 {
		t.Errorf("nome longo não foi cortado: %d runas", len([]rune(got)))
	}
	if got := sanitizeName("a\nb\tc"); got != "abc" {
		t.Errorf("sanitizeName = %q, queria abc", got)
	}
}

// A description do .meta.json chega ao snapshot: o mini-tile a usa como rótulo.
func TestDescricaoDoSubagenteNoSnapshot(t *testing.T) {
	s := New()
	startSession(s, fixture(t))
	NewRefresher(s).RefreshOnce()
	got := map[string]string{}
	for _, v := range s.Snapshot() {
		for _, a := range v.Subagents {
			got[a.ID] = a.Description
		}
	}
	if got["a1"] != "procura arquivos" || got["a2"] != "escreve testes" {
		t.Errorf("descrições no snapshot = %v", got)
	}
}
