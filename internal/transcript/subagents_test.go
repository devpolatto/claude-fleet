package transcript

import (
	"os"
	"path/filepath"
	"testing"
)

// writeFile grava s em path, criando os diretórios intermediários.
func writeFile(t *testing.T, path, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
}

// sessaoTemp monta <tmp>/<sid>.jsonl e devolve o caminho do transcript e o
// diretório subagents correspondente (ainda não criado).
func sessaoTemp(t *testing.T) (transcriptPath, subDir string) {
	t.Helper()
	dir := t.TempDir()
	transcriptPath = filepath.Join(dir, "sess-x.jsonl")
	writeFile(t, transcriptPath, "")
	return transcriptPath, filepath.Join(dir, "sess-x", "subagents")
}

func TestSubagentsOfFixtureComDois(t *testing.T) {
	got := SubagentsOf(filepath.Join("testdata", "subagents", "sess-dois.jsonl"))
	if len(got) != 2 {
		t.Fatalf("len = %d, queria 2: %+v", len(got), got)
	}
	dir, err := filepath.Abs(filepath.Join("testdata", "subagents", "sess-dois", "subagents"))
	if err != nil {
		t.Fatal(err)
	}
	want := []SubagentFile{
		{AgentID: "a1", TranscriptPath: filepath.Join(dir, "agent-a1.jsonl"), Type: "Explore", ToolUseID: "toolu_sintetico_a1"},
		{AgentID: "b2", TranscriptPath: filepath.Join(dir, "agent-b2.jsonl"), Type: "general-purpose", ToolUseID: "toolu_sintetico_b2"},
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] = %+v, queria %+v", i, got[i], want[i])
		}
		if !filepath.IsAbs(got[i].TranscriptPath) {
			t.Errorf("[%d] TranscriptPath não é absoluto: %s", i, got[i].TranscriptPath)
		}
	}
}

func TestSubagentsOfMetaAusenteOuInvalido(t *testing.T) {
	tp, sub := sessaoTemp(t)
	writeFile(t, filepath.Join(sub, "agent-ok.jsonl"), "{}\n")
	writeFile(t, filepath.Join(sub, "agent-ok.meta.json"), `{"agentType":"Plan","description":"ok"}`)
	writeFile(t, filepath.Join(sub, "agent-semmeta.jsonl"), "{}\n")
	writeFile(t, filepath.Join(sub, "agent-quebrado.jsonl"), "{}\n")
	writeFile(t, filepath.Join(sub, "agent-quebrado.meta.json"), `{"agentType":"Plan","descr`)
	writeFile(t, filepath.Join(sub, "agent-tipoerrado.jsonl"), "{}\n")
	writeFile(t, filepath.Join(sub, "agent-tipoerrado.meta.json"), `{"agentType":42}`)

	got := SubagentsOf(tp)
	if len(got) != 4 {
		t.Fatalf("len = %d, queria 4: %+v", len(got), got)
	}
	byID := map[string]SubagentFile{}
	for _, s := range got {
		byID[s.AgentID] = s
	}
	if s := byID["ok"]; s.Type != "Plan" {
		t.Errorf("ok = %+v, queria o tipo", s)
	}
	for _, id := range []string{"semmeta", "quebrado", "tipoerrado"} {
		s, ok := byID[id]
		if !ok {
			t.Errorf("%s ausente do resultado", id)
			continue
		}
		if s.Type != "" || s.ToolUseID != "" {
			t.Errorf("%s = %+v, queria metadados vazios", id, s)
		}
		if s.TranscriptPath != filepath.Join(sub, "agent-"+id+".jsonl") {
			t.Errorf("%s TranscriptPath = %s", id, s.TranscriptPath)
		}
	}
}

func TestSubagentsOfDiretorioInexistente(t *testing.T) {
	tp, _ := sessaoTemp(t)
	if got := SubagentsOf(tp); got != nil {
		t.Errorf("sem diretório subagents: %+v, queria nil", got)
	}
	if got := SubagentsOf(filepath.Join(t.TempDir(), "nao", "existe.jsonl")); got != nil {
		t.Errorf("transcript inexistente: %+v, queria nil", got)
	}
	if got := SubagentsOf(""); got != nil {
		t.Errorf("caminho vazio: %+v, queria nil", got)
	}
}

func TestSubagentsOfIgnoraForaDoPadrao(t *testing.T) {
	tp, sub := sessaoTemp(t)
	writeFile(t, filepath.Join(sub, "agent-z9.jsonl"), "{}\n")
	writeFile(t, filepath.Join(sub, "agent-z9.meta.json"), `{"agentType":"Explore"}`)
	writeFile(t, filepath.Join(sub, "agent-.jsonl"), "{}\n")             // id vazio
	writeFile(t, filepath.Join(sub, "outro.jsonl"), "{}\n")              // sem prefixo
	writeFile(t, filepath.Join(sub, "agent-y8.json"), "{}\n")            // extensão errada
	writeFile(t, filepath.Join(sub, "agent-x7.jsonl.bak"), "{}\n")       // sufixo extra
	writeFile(t, filepath.Join(sub, "notas.txt"), "ruído\n")             // qualquer outro
	writeFile(t, filepath.Join(sub, "agent-w6.jsonl", "dentro"), "{}\n") // diretório com nome de agente

	got := SubagentsOf(tp)
	if len(got) != 1 || got[0].AgentID != "z9" || got[0].Type != "Explore" {
		t.Errorf("got %+v, queria só z9 (Explore)", got)
	}
}

func TestSubagentsOfOrdemPorAgentID(t *testing.T) {
	tp, sub := sessaoTemp(t)
	for _, id := range []string{"c", "a", "b10", "b2"} {
		writeFile(t, filepath.Join(sub, "agent-"+id+".jsonl"), "{}\n")
	}
	got := SubagentsOf(tp)
	want := []string{"a", "b10", "b2", "c"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, queria %d", len(got), len(want))
	}
	for i, id := range want {
		if got[i].AgentID != id {
			t.Errorf("[%d] = %s, queria %s", i, got[i].AgentID, id)
		}
	}
}
