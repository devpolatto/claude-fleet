package ratelimit

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func lerFixture(t *testing.T, nome string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", nome))
	if err != nil {
		t.Fatalf("lendo fixture %s: %v", nome, err)
	}
	return b
}

func TestParseGuardaSoRateLimits(t *testing.T) {
	casos := []struct {
		fixture  string
		caminho  string
		fiveHour *Window
		sevenDay *Window
	}{
		{
			fixture:  "statusline-completo.json",
			caminho:  "/home/usuario/.claude/projects/-home-usuario-projeto/abc.jsonl",
			fiveHour: &Window{UsedPercentage: 23.5, ResetsAt: 1792171200},
			sevenDay: &Window{UsedPercentage: 41, ResetsAt: 1792603200},
		},
		{
			fixture: "statusline-sem-rate-limits.json",
			caminho: "/home/usuario/.claude/projects/-home-usuario-projeto/def.jsonl",
		},
		{
			fixture:  "statusline-so-semana.json",
			caminho:  "/home/usuario/.claude/projects/-home-usuario-projeto/ghi.jsonl",
			sevenDay: &Window{UsedPercentage: 64.5, ResetsAt: 1792603200},
		},
	}
	for _, c := range casos {
		t.Run(c.fixture, func(t *testing.T) {
			r, err := Parse(bytes.NewReader(lerFixture(t, c.fixture)), MaxBytes)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			want := Reading{TranscriptPath: c.caminho, FiveHour: c.fiveHour, SevenDay: c.sevenDay}
			if !reflect.DeepEqual(r, want) {
				t.Fatalf("Parse = %+v, quer %+v", r, want)
			}
		})
	}
}

// Reading não pode ganhar campo além das janelas e do caminho, e o caminho
// não pode ir para JSON.
func TestReadingSoTemJanelasECaminho(t *testing.T) {
	tipo := reflect.TypeOf(Reading{})
	permitidos := map[string]bool{"TranscriptPath": true, "FiveHour": true, "SevenDay": true}
	if tipo.NumField() != len(permitidos) {
		t.Fatalf("Reading tem %d campos, quer %d", tipo.NumField(), len(permitidos))
	}
	for i := range tipo.NumField() {
		f := tipo.Field(i)
		if !permitidos[f.Name] {
			t.Errorf("campo inesperado em Reading: %s", f.Name)
		}
		if f.Name == "TranscriptPath" && f.Tag.Get("json") != "" {
			t.Errorf("TranscriptPath não pode ter tag JSON: %q", f.Tag)
		}
	}
}

// Nada do resto do JSON (cwd, session_id, modelo, custo, spend_limit) pode
// sobreviver ao Parse.
func TestParseNaoVazaOResto(t *testing.T) {
	r, err := Parse(bytes.NewReader(lerFixture(t, "statusline-completo.json")), MaxBytes)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	r.TranscriptPath = ""
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for _, proibido := range []string{"00000000-0000-4000-8000", "/home/usuario", "Opus", "spend", "0.4213", "12.5", "context"} {
		if strings.Contains(string(b), proibido) {
			t.Errorf("Reading serializada contém %q: %s", proibido, b)
		}
	}
}

func TestParseDescartaJanelaInvalida(t *testing.T) {
	casos := []struct {
		nome   string
		janela string // valor de rate_limits.five_hour
		want   *Window
	}{
		{"válida inteira", `{"used_percentage": 37, "resets_at": 1792171200}`, &Window{37, 1792171200}},
		{"válida decimal", `{"used_percentage": 23.5, "resets_at": 1792171200}`, &Window{23.5, 1792171200}},
		{"zero por cento", `{"used_percentage": 0, "resets_at": 1792171200}`, &Window{0, 1792171200}},
		{"cem por cento", `{"used_percentage": 100, "resets_at": 1792171200}`, &Window{100, 1792171200}},
		{"resets_at decimal é truncado", `{"used_percentage": 5, "resets_at": 1792171200.7}`, &Window{5, 1792171200}},
		{"percentual negativo", `{"used_percentage": -0.1, "resets_at": 1792171200}`, nil},
		{"percentual acima de 100", `{"used_percentage": 100.5, "resets_at": 1792171200}`, nil},
		{"resets_at zero", `{"used_percentage": 10, "resets_at": 0}`, nil},
		{"resets_at negativo", `{"used_percentage": 10, "resets_at": -5}`, nil},
		{"resets_at absurdo", `{"used_percentage": 10, "resets_at": 1e300}`, nil},
		{"sem used_percentage", `{"resets_at": 1792171200}`, nil},
		{"sem resets_at", `{"used_percentage": 10}`, nil},
		{"used_percentage null", `{"used_percentage": null, "resets_at": 1792171200}`, nil},
		{"resets_at null", `{"used_percentage": 10, "resets_at": null}`, nil},
		{"janela null", `null`, nil},
		{"janela vazia", `{}`, nil},
	}
	semana := &Window{UsedPercentage: 12, ResetsAt: 1792603200}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			corpo := `{"transcript_path": "/x.jsonl", "rate_limits": {"five_hour": ` + c.janela +
				`, "seven_day": {"used_percentage": 12, "resets_at": 1792603200}}}`
			r, err := Parse(strings.NewReader(corpo), MaxBytes)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if !reflect.DeepEqual(r.FiveHour, c.want) {
				t.Errorf("FiveHour = %+v, quer %+v", r.FiveHour, c.want)
			}
			if !reflect.DeepEqual(r.SevenDay, semana) {
				t.Errorf("SevenDay = %+v, quer %+v (janela inválida não pode afetar a outra)", r.SevenDay, semana)
			}
		})
	}
}

func TestParseRecusaCorpoGrande(t *testing.T) {
	completo := lerFixture(t, "statusline-completo.json")
	casos := []struct {
		nome   string
		corpo  []byte
		max    int64
		recusa bool
	}{
		{"cabe exato", completo, int64(len(completo)), false},
		{"um byte acima", completo, int64(len(completo)) - 1, true},
		{"muito acima do MaxBytes", append([]byte(`{"x":"`), bytes.Repeat([]byte("a"), MaxBytes+10)...), MaxBytes, true},
		{"limite negativo", []byte(`{}`), -1, true},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			_, err := Parse(bytes.NewReader(c.corpo), c.max)
			if got := errors.Is(err, ErrTooLarge); got != c.recusa {
				t.Fatalf("errors.Is(err, ErrTooLarge) = %v, quer %v (err = %v)", got, c.recusa, err)
			}
		})
	}
}

// leitorContado conta quantos bytes foram pedidos, para provar que o Parse
// não lê além de max+1.
type leitorContado struct {
	lidos int64
}

func (l *leitorContado) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = ' '
	}
	l.lidos += int64(len(p))
	return len(p), nil
}

func TestParseNaoLeAlemDoLimite(t *testing.T) {
	l := &leitorContado{}
	if _, err := Parse(l, 1024); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, quer ErrTooLarge", err)
	}
	if l.lidos > 1025 {
		t.Fatalf("leu %d bytes, limite era 1025", l.lidos)
	}
}

func TestParseRecusaJSONInvalido(t *testing.T) {
	segredo := "segredo-do-corpo"
	casos := []struct {
		nome  string
		corpo string
	}{
		{"vazio", ``},
		{"truncado", `{"transcript_path": "` + segredo},
		{"não é objeto", `["` + segredo + `"]`},
		{"string solta", `"` + segredo + `"`},
		{"rate_limits com tipo errado", `{"rate_limits": "` + segredo + `"}`},
		{"used_percentage string", `{"rate_limits": {"five_hour": {"used_percentage": "` + segredo + `", "resets_at": 1}}}`},
		{"transcript_path número", `{"transcript_path": 123456789}`},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			_, err := Parse(strings.NewReader(c.corpo), MaxBytes)
			if !errors.Is(err, ErrInvalidJSON) {
				t.Fatalf("err = %v, quer ErrInvalidJSON", err)
			}
			if strings.Contains(err.Error(), segredo) || strings.Contains(err.Error(), "123456789") {
				t.Fatalf("erro cita o corpo: %v", err)
			}
		})
	}
}

// Campos fora da lista branca, mesmo com tipo estranho, não derrubam o Parse.
func TestParseIgnoraCamposForaDaLista(t *testing.T) {
	corpo := `{"cwd": 1, "model": "x", "spend_limit": [1, 2], "rate_limits": {"five_hour": {"used_percentage": 1, "resets_at": 2}, "opus_week": "x"}}`
	r, err := Parse(strings.NewReader(corpo), MaxBytes)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := Reading{FiveHour: &Window{1, 2}}
	if !reflect.DeepEqual(r, want) {
		t.Fatalf("Parse = %+v, quer %+v", r, want)
	}
}
