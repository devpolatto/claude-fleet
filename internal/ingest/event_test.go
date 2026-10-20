package ingest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sentinela é a string colocada nos campos de conteúdo das fixtures; ela
// nunca pode aparecer no Event nem em mensagens de erro.
const sentinela = "SENTINELA-CONTEUDO"

const limite = 256 << 10 // mesmo limite do servidor (256 KiB)

const (
	sessao     = "sess-123"
	cwd        = "/tmp/proj"
	transcript = "/home/u/.claude/projects/-tmp-proj/sess-123.jsonl"
)

// base devolve o Event com os campos comuns a todas as fixtures.
func base(name string) Event {
	return Event{Name: name, SessionID: sessao, Cwd: cwd, TranscriptPath: transcript}
}

// assertSemSentinela falha se o Event, serializado de qualquer forma, citar
// o conteúdo de trabalho.
func assertSemSentinela(t *testing.T, ev Event) {
	t.Helper()
	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if bytes.Contains(b, []byte(sentinela)) {
		t.Errorf("json.Marshal(Event) contém conteúdo: %s", b)
	}
	if s := fmt.Sprintf("%+v", ev); strings.Contains(s, sentinela) {
		t.Errorf("%%+v do Event contém conteúdo: %s", s)
	}
	if s := fmt.Sprintf("%#v", ev); strings.Contains(s, sentinela) {
		t.Errorf("%%#v do Event contém conteúdo: %s", s)
	}
}

func TestParseFixtures(t *testing.T) {
	sub := func(ev Event) Event {
		ev.AgentID = "agent-7"
		ev.AgentType = "general-purpose"
		return ev
	}
	tests := []struct {
		arquivo string
		want    Event
	}{
		{"session_start.json", func() Event { e := base("SessionStart"); e.Source = "startup"; return e }()},
		{"user_prompt_submit.json", func() Event { e := base("UserPromptSubmit"); e.PermissionMode = "default"; return e }()},
		{"pre_tool_use.json", func() Event {
			e := base("PreToolUse")
			e.ToolName, e.ToolUseID, e.PermissionMode = "Bash", "toolu_01", "default"
			return e
		}()},
		{"pre_tool_use_subagent.json", func() Event {
			e := sub(base("PreToolUse"))
			e.ToolName, e.ToolUseID, e.PermissionMode = "Bash", "toolu_02", "default"
			return e
		}()},
		{"post_tool_use.json", func() Event {
			e := sub(base("PostToolUse"))
			e.ToolName, e.ToolUseID, e.PermissionMode = "Bash", "toolu_02", "default"
			return e
		}()},
		{"post_tool_use_failure.json", func() Event {
			e := base("PostToolUseFailure")
			e.ToolName, e.ToolUseID, e.PermissionMode = "Bash", "toolu_03", "default"
			return e
		}()},
		{"post_tool_batch.json", func() Event {
			e := sub(base("PostToolBatch"))
			e.PermissionMode = "default"
			return e
		}()},
		{"permission_request.json", func() Event {
			e := base("PermissionRequest")
			e.ToolName, e.PermissionMode = "Write", "default"
			return e
		}()},
		{"stop.json", func() Event { e := base("Stop"); e.PermissionMode = "default"; return e }()},
		{"subagent_start.json", sub(base("SubagentStart"))},
		{"subagent_stop.json", func() Event {
			e := sub(base("SubagentStop"))
			e.PermissionMode = "default"
			e.AgentTranscriptPath = "/home/u/.claude/projects/-tmp-proj/sess-123/subagents/agent-agent-7.jsonl"
			return e
		}()},
		{"pre_compact.json", func() Event { e := base("PreCompact"); e.Trigger = "manual"; return e }()},
		{"post_compact.json", func() Event { e := base("PostCompact"); e.Trigger = "manual"; return e }()},
		{"session_end.json", func() Event { e := base("SessionEnd"); e.Reason = "other"; return e }()},
		{"notification.json", base("Notification")},
	}
	for _, tt := range tests {
		t.Run(tt.arquivo, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("testdata", tt.arquivo))
			if err != nil {
				t.Fatalf("lendo fixture: %v", err)
			}
			if tt.arquivo != "session_start.json" && tt.arquivo != "subagent_start.json" &&
				tt.arquivo != "session_end.json" && !bytes.Contains(raw, []byte(sentinela)) {
				t.Fatalf("fixture %s deveria conter a sentinela", tt.arquivo)
			}
			ev, err := Parse(bytes.NewReader(raw), limite)
			if err != nil {
				t.Fatalf("Parse: erro inesperado: %v", err)
			}
			assertSemSentinela(t, ev)
			if ev != tt.want {
				t.Errorf("Event = %+v\nqueria   %+v", ev, tt.want)
			}
		})
	}
}

// TestParseTodasAsFixtures garante que nenhuma fixture nova fique de fora da
// verificação da sentinela.
func TestParseTodasAsFixtures(t *testing.T) {
	arquivos, err := filepath.Glob(filepath.Join("testdata", "*.json"))
	if err != nil || len(arquivos) == 0 {
		t.Fatalf("nenhuma fixture encontrada: %v", err)
	}
	for _, a := range arquivos {
		raw, err := os.ReadFile(a)
		if err != nil {
			t.Fatal(err)
		}
		ev, err := Parse(bytes.NewReader(raw), limite)
		if err != nil {
			t.Errorf("%s: erro inesperado: %v", a, err)
			continue
		}
		assertSemSentinela(t, ev)
	}
}

func TestParseTooLarge(t *testing.T) {
	corpo := `{"hook_event_name":"Stop","session_id":"s","last_assistant_message":"` + sentinela + `"}`
	n := int64(len(corpo))

	if _, err := Parse(strings.NewReader(corpo), n); err != nil {
		t.Fatalf("corpo com exatamente maxBytes: erro inesperado: %v", err)
	}

	_, err := Parse(strings.NewReader(corpo), n-1)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("erro = %v, queria ErrTooLarge", err)
	}
	if strings.Contains(err.Error(), sentinela) {
		t.Errorf("mensagem de erro ecoa o corpo: %v", err)
	}
}

// leitorContado conta quantos bytes foram lidos da fonte.
type leitorContado struct {
	r *strings.Reader
	n int64
}

func (l *leitorContado) Read(p []byte) (int, error) {
	k, err := l.r.Read(p)
	l.n += int64(k)
	return k, err
}

func TestParseTooLargeNaoLeAlemDoLimite(t *testing.T) {
	const max = 1024
	src := &leitorContado{r: strings.NewReader(strings.Repeat("x", 10*max))}
	if _, err := Parse(src, max); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("erro = %v, queria ErrTooLarge", err)
	}
	if src.n > max+1 {
		t.Errorf("leu %d bytes, não deveria passar de %d", src.n, max+1)
	}
}

func TestParseErros(t *testing.T) {
	tests := []struct {
		name  string
		corpo string
		want  error
	}{
		{"sem hook_event_name", `{"session_id":"s","prompt":"` + sentinela + `"}`, ErrMissingEvent},
		{"hook_event_name vazio", `{"hook_event_name":"","session_id":"s"}`, ErrMissingEvent},
		{"null", `null`, ErrMissingEvent},
		{"objeto vazio", `{}`, ErrMissingEvent},
		{"sem session_id", `{"hook_event_name":"Stop","last_assistant_message":"` + sentinela + `"}`, ErrMissingSession},
		{"session_id vazio", `{"hook_event_name":"Stop","session_id":""}`, ErrMissingSession},
		{"corpo vazio", ``, ErrInvalidJSON},
		{"JSON truncado", `{"hook_event_name":"Stop","prompt":"` + sentinela, ErrInvalidJSON},
		{"sintaxe inválida", `{"hook_event_name":"Stop", "prompt": ` + sentinela + `}`, ErrInvalidJSON},
		{"lixo depois do objeto", `{"hook_event_name":"Stop","session_id":"s"} ` + sentinela, ErrInvalidJSON},
		{"array no topo", `["` + sentinela + `"]`, ErrInvalidJSON},
		{"string no topo", `"` + sentinela + `"`, ErrInvalidJSON},
		{"tipo errado em campo da lista branca", `{"hook_event_name":"Stop","session_id":"s","tool_name":{"x":"` + sentinela + `"}}`, ErrInvalidJSON},
		{"número em campo da lista branca", `{"hook_event_name":"Stop","session_id":"s","cwd":4242424242}`, ErrInvalidJSON},
		{"cwd maior que MaxFieldBytes", `{"hook_event_name":"Stop","session_id":"s","cwd":"/` + strings.Repeat("a", MaxFieldBytes) + `"}`, ErrFieldTooLong},
		{"transcript_path maior que MaxFieldBytes", `{"hook_event_name":"Stop","session_id":"s","transcript_path":"/` + strings.Repeat("a", MaxFieldBytes) + `"}`, ErrFieldTooLong},
		{"session_id maior que MaxFieldBytes", `{"hook_event_name":"Stop","session_id":"` + strings.Repeat("s", MaxFieldBytes+1) + `"}`, ErrFieldTooLong},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev, err := Parse(strings.NewReader(tt.corpo), limite)
			if !errors.Is(err, tt.want) {
				t.Fatalf("erro = %v, queria %v", err, tt.want)
			}
			if strings.Contains(err.Error(), sentinela) || strings.Contains(err.Error(), "4242424242") {
				t.Errorf("mensagem de erro ecoa o corpo: %v", err)
			}
			if ev != (Event{}) {
				t.Errorf("Event deveria vir zerado em erro, veio %+v", ev)
			}
		})
	}
}

// Campo com exatamente MaxFieldBytes passa: é o tamanho de PATH_MAX.
func TestParseCampoNoLimite(t *testing.T) {
	cwd := "/" + strings.Repeat("a", MaxFieldBytes-1)
	ev, err := Parse(strings.NewReader(`{"hook_event_name":"Stop","session_id":"s","cwd":"`+cwd+`"}`), limite)
	if err != nil || ev.Cwd != cwd {
		t.Fatalf("erro = %v, len(Cwd) = %d", err, len(ev.Cwd))
	}
}

// Campos fora da lista branca com nomes parecidos, aninhados ou com tipos
// arbitrários são ignorados sem erro.
func TestParseIgnoraCamposDesconhecidos(t *testing.T) {
	corpo := `{
		"hook_event_name": "PreToolUse",
		"session_id": "s",
		"tool_name": "Bash",
		"tool_input": {"command": "` + sentinela + `", "nested": [1, {"a": "` + sentinela + `"}]},
		"tool_response": null,
		"effort": {"level": "high"},
		"duration_ms": 12,
		"is_interrupt": true,
		"extra": "` + sentinela + `"
	}`
	ev, err := Parse(strings.NewReader(corpo), limite)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	assertSemSentinela(t, ev)
	want := Event{Name: "PreToolUse", SessionID: "s", ToolName: "Bash"}
	if ev != want {
		t.Errorf("Event = %+v, queria %+v", ev, want)
	}
}
