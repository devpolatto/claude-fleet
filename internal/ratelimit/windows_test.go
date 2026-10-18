package ratelimit

import (
	"encoding/json"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/devpolatto/claude-fleet/internal/pricing"
)

// agoraJanelas é o "agora" injetado nos testes das janelas; os timestamps
// das fixtures em testdata/projects são fixos em torno dele.
var agoraJanelas = time.Date(2026, 10, 18, 14, 0, 0, 0, time.UTC)

const arquivoVelho = "-proj-b/sessao-velha.jsonl"

// copiarFixtures copia testdata/projects para um diretório temporário e
// ajusta o mtime: tudo um minuto antes de agoraJanelas, menos o arquivo
// velho, com oito dias.
func copiarFixtures(t *testing.T) string {
	t.Helper()
	dst := filepath.Join(t.TempDir(), "projects")
	src := filepath.Join("testdata", "projects")
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		alvo := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(alvo, 0o700)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if err := os.WriteFile(alvo, data, 0o600); err != nil {
			return err
		}
		mtime := agoraJanelas.Add(-time.Minute)
		if filepath.ToSlash(rel) == arquivoVelho {
			mtime = agoraJanelas.Add(-8 * 24 * time.Hour)
		}
		return os.Chtimes(alvo, mtime, mtime)
	})
	if err != nil {
		t.Fatalf("copiando fixtures: %v", err)
	}
	return dst
}

// Contagem manual sobre as fixtures (preço de lista em pricing):
//
//	msg_a1  opus-5-5   18/10 12:00:01  in 10, c5m 100, read 1000, out 50 (2 linhas: vale a última)
//	msg_a2  opus-5-5   15/10 10:00     in 20, read 2000, out 200, fast
//	msg_a3  opus-5-5   10/10 10:00     fora dos 7 dias
//	msg_s1  haiku-4-5  18/10 13:30     in 3, c5m 40, out 30 (subagente)
//	msg_b1  sonnet-5-5 18/10 10:30     in 7, c1h 60, read 500, out 70
//	msg_b2  modelo-x   12/10 10:00     in 11, out 11 (sem preço)
//	msg_syn <synthetic> 18/10 13:00    tudo zero (ignorado)
//	msg_v1  opus-5-5   17/10 10:00     arquivo com mtime de 8 dias: ignorado
var (
	custoA1 = (10*4.00 + 100*4.00*1.25 + 1000*0.20 + 50*20.00) / 1e6
	custoA2 = 2 * (20*4.00 + 2000*0.20 + 200*20.00) / 1e6
	custoS1 = (3*1.00 + 40*1.00*1.25 + 30*5.00) / 1e6
	custoB1 = (7*2.00 + 60*2.00*2 + 500*0.20 + 70*10.00) / 1e6
)

func mesmaSoma(t *testing.T, nome string, got, want WindowSum) {
	t.Helper()
	if got.Tokens != want.Tokens || got.CostKnown != want.CostKnown || math.Abs(got.CostUSD-want.CostUSD) > 1e-12 {
		t.Errorf("%s = %+v, queria %+v", nome, got, want)
	}
}

func janelasDe(t *testing.T, tr *Tracker, conta string) WindowTotals {
	t.Helper()
	for _, e := range tr.Snapshot() {
		if e.Account == conta {
			if e.Windows == nil {
				t.Fatalf("conta %q sem janelas", conta)
			}
			return *e.Windows
		}
	}
	t.Fatalf("conta %q ausente", conta)
	return WindowTotals{}
}

func TestWindowsSomaContraContagemManual(t *testing.T) {
	dir := copiarFixtures(t)
	tr := carregar(t, "", nil, novoRelogio())
	w := NewWindows(map[string]string{"pessoal": dir}, tr)
	w.RefreshOnce(agoraJanelas)

	got := janelasDe(t, tr, "pessoal")
	mesmaSoma(t, "five_hour", got.FiveHour, WindowSum{
		Tokens:    pricing.Tokens{Input: 20, Cache5m: 140, Cache1h: 60, CacheRead: 1500, Output: 150},
		CostUSD:   custoA1 + custoS1 + custoB1,
		CostKnown: true,
	})
	mesmaSoma(t, "seven_day", got.SevenDay, WindowSum{
		Tokens:    pricing.Tokens{Input: 51, Cache5m: 140, Cache1h: 60, CacheRead: 3500, Output: 361},
		CostUSD:   custoA1 + custoA2 + custoS1 + custoB1,
		CostKnown: false, // modelo-x sem preço
	})

	// Segunda volta sem mudança no disco: mesma soma, sem ler de novo do zero.
	w.RefreshOnce(agoraJanelas)
	if again := janelasDe(t, tr, "pessoal"); again != got {
		t.Errorf("segunda volta = %+v, queria %+v", again, got)
	}
}

func TestWindowsUsaInicioDaJanelaDoPlano(t *testing.T) {
	agora := agoraJanelas.Unix()
	semPlano := WindowTotals{
		FiveHour: WindowSum{
			Tokens:  pricing.Tokens{Input: 20, Cache5m: 140, Cache1h: 60, CacheRead: 1500, Output: 150},
			CostUSD: custoA1 + custoS1 + custoB1, CostKnown: true,
		},
		SevenDay: WindowSum{
			Tokens:  pricing.Tokens{Input: 51, Cache5m: 140, Cache1h: 60, CacheRead: 3500, Output: 361},
			CostUSD: custoA1 + custoA2 + custoS1 + custoB1, CostKnown: false,
		},
	}
	tests := []struct {
		name string
		r    *Reading
		want WindowTotals
	}{
		{"sem dado do plano usa as últimas 5 h e 7 dias", nil, semPlano},
		{
			// 5 h: começa às 13:00 (só msg_s1); 7 dias: começa em 13/10 14:00 (sem msg_b2).
			"plano em curso define o início",
			&Reading{FiveHour: janela(40, agora+4*3600), SevenDay: janela(10, agora+2*24*3600)},
			WindowTotals{
				FiveHour: WindowSum{Tokens: pricing.Tokens{Input: 3, Cache5m: 40, Output: 30}, CostUSD: custoS1, CostKnown: true},
				SevenDay: WindowSum{
					Tokens:  pricing.Tokens{Input: 40, Cache5m: 140, Cache1h: 60, CacheRead: 3500, Output: 350},
					CostUSD: custoA1 + custoA2 + custoS1 + custoB1, CostKnown: true,
				},
			},
		},
		{
			"plano vencido volta às janelas corridas",
			&Reading{FiveHour: janela(40, agora-3600), SevenDay: janela(10, agora-3600)},
			semPlano,
		},
		{
			"resets_at absurdo (além da duração da janela) é ignorado",
			&Reading{FiveHour: janela(40, agora+10*3600), SevenDay: janela(10, agora+30*24*3600)},
			semPlano,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := copiarFixtures(t)
			tr := carregar(t, "", []string{"pessoal"}, novoRelogio())
			if tt.r != nil {
				tr.Apply("pessoal", *tt.r)
			}
			NewWindows(map[string]string{"pessoal": dir}, tr).RefreshOnce(agoraJanelas)
			got := janelasDe(t, tr, "pessoal")
			mesmaSoma(t, "five_hour", got.FiveHour, tt.want.FiveHour)
			mesmaSoma(t, "seven_day", got.SevenDay, tt.want.SevenDay)
		})
	}
}

func TestWindowsIgnoraArquivoMaisVelhoQue7Dias(t *testing.T) {
	dir := copiarFixtures(t)
	tr := carregar(t, "", nil, novoRelogio())
	w := NewWindows(map[string]string{"": dir}, tr)
	w.RefreshOnce(agoraJanelas)

	velho := filepath.Join(dir, filepath.FromSlash(arquivoVelho))
	if _, ok := w.files[velho]; ok {
		t.Error("arquivo com mtime de 8 dias entrou no cache")
	}
	if got := janelasDe(t, tr, "").SevenDay.Tokens.Input; got != 51 {
		t.Errorf("seven_day input = %d, queria 51 (sem o arquivo velho)", got)
	}

	// Com mtime atual, o mesmo arquivo passa a contar.
	if err := os.Chtimes(velho, agoraJanelas, agoraJanelas); err != nil {
		t.Fatal(err)
	}
	w.RefreshOnce(agoraJanelas)
	if got := janelasDe(t, tr, "").SevenDay.Tokens.Input; got != 5051 {
		t.Errorf("seven_day input = %d, queria 5051 com o arquivo atualizado", got)
	}
}

func TestWindowsSoltaArquivoSumidoOuForaDaJanela(t *testing.T) {
	dir := copiarFixtures(t)
	tr := carregar(t, "", nil, novoRelogio())
	w := NewWindows(map[string]string{"": dir}, tr)
	w.RefreshOnce(agoraJanelas)
	if len(w.files) != 3 {
		t.Fatalf("%d arquivos no cache, queria 3", len(w.files))
	}

	if err := os.Remove(filepath.Join(dir, "-proj-b", "sessao-b.jsonl")); err != nil {
		t.Fatal(err)
	}
	w.RefreshOnce(agoraJanelas)
	if len(w.files) != 2 {
		t.Errorf("%d arquivos depois de apagar um, queria 2", len(w.files))
	}

	// Oito dias depois, nada mais está na janela.
	w.RefreshOnce(agoraJanelas.Add(8 * 24 * time.Hour))
	if len(w.files) != 0 {
		t.Errorf("%d arquivos fora da janela no cache, queria 0", len(w.files))
	}
	if got := janelasDe(t, tr, ""); got != (WindowTotals{FiveHour: WindowSum{CostKnown: true}, SevenDay: WindowSum{CostKnown: true}}) {
		t.Errorf("janelas = %+v, queria zeradas", got)
	}
}

func TestWindowsDiretorioInexistenteFicaZerado(t *testing.T) {
	tr := carregar(t, "", nil, novoRelogio())
	w := NewWindows(map[string]string{"x": filepath.Join(t.TempDir(), "nao-existe")}, tr)
	w.RefreshOnce(agoraJanelas)
	if got := janelasDe(t, tr, "x"); got.FiveHour.Tokens != (pricing.Tokens{}) || got.SevenDay.Tokens != (pricing.Tokens{}) {
		t.Errorf("janelas = %+v, queria zeradas", got)
	}
}

func TestSetWindowsAvisaSoNaMudanca(t *testing.T) {
	tr := carregar(t, "", []string{"pessoal"}, novoRelogio())
	ch := tr.Subscribe()
	a := WindowTotals{FiveHour: WindowSum{Tokens: pricing.Tokens{Input: 1}, CostUSD: 0.5, CostKnown: true}}
	b := a
	b.SevenDay.Tokens.Output = 9

	passos := []struct {
		name  string
		conta string
		w     WindowTotals
		aviso bool
	}{
		{"primeiro valor avisa", "pessoal", a, true},
		{"mesmo valor não avisa", "pessoal", a, false},
		{"valor novo avisa", "pessoal", b, true},
		{"mesmo valor de novo não avisa", "pessoal", b, false},
		{"conta nova é criada e avisa", "outra", a, true},
	}
	for _, p := range passos {
		changed := tr.SetWindows(p.conta, p.w)
		if changed != p.aviso || recebeu(ch) != p.aviso {
			t.Errorf("%s: changed=%t, queria aviso=%t", p.name, changed, p.aviso)
		}
	}

	// Snapshot devolve cópia: mexer nela não muda o estado.
	snap := tr.Snapshot()
	snap[1].Windows.FiveHour.CostUSD = 99
	if got := janelasDe(t, tr, "pessoal"); got != b {
		t.Errorf("estado mudou pela cópia: %+v", got)
	}
}

func TestUsoPlanoJSONSemWindows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "uso-plano.json")
	tr := carregar(t, path, []string{"pessoal"}, novoRelogio())

	tr.SetWindows("pessoal", WindowTotals{FiveHour: WindowSum{Tokens: pricing.Tokens{Input: 1}, CostKnown: true}})
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("SetWindows gravou o arquivo (stat: %v)", err)
	}

	tr.Apply("pessoal", Reading{FiveHour: janela(10, 1792171200)})
	tr.SetWindows("pessoal", WindowTotals{FiveHour: WindowSum{Tokens: pricing.Tokens{Input: 2}, CostKnown: true}})
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "windows") || strings.Contains(string(data), "cost_usd") {
		t.Errorf("uso-plano.json traz as janelas:\n%s", data)
	}

	// Na API, as janelas aparecem com as tags snake_case.
	out, err := json.Marshal(tr.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	for _, chave := range []string{`"windows"`, `"five_hour"`, `"seven_day"`, `"tokens"`, `"cost_usd"`, `"cost_known"`} {
		if !strings.Contains(string(out), chave) {
			t.Errorf("Snapshot em JSON sem %s: %s", chave, out)
		}
	}
}

func TestWindowsContaUmaVezMensagemRepetidaEntreArquivos(t *testing.T) {
	dir := copiarFixtures(t)
	tr := carregar(t, "", nil, novoRelogio())
	w := NewWindows(map[string]string{"": dir}, tr)
	w.RefreshOnce(agoraJanelas)
	antes := janelasDe(t, tr, "")

	// Sessão retomada: um arquivo novo repete as mensagens de outro, com os
	// mesmos message.id e timestamps.
	orig := filepath.Join(dir, "-proj-a", "sessao-a.jsonl")
	data, err := os.ReadFile(orig)
	if err != nil {
		t.Fatal(err)
	}
	copia := filepath.Join(dir, "-proj-a", "sessao-retomada.jsonl")
	if err := os.WriteFile(copia, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(copia, agoraJanelas, agoraJanelas); err != nil {
		t.Fatal(err)
	}
	w.RefreshOnce(agoraJanelas)
	depois := janelasDe(t, tr, "")
	mesmaSoma(t, "five_hour", depois.FiveHour, antes.FiveHour)
	mesmaSoma(t, "seven_day", depois.SevenDay, antes.SevenDay)
}
