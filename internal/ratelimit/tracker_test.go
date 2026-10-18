package ratelimit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// relogio é um relógio injetável para os testes.
type relogio struct {
	mu sync.Mutex
	t  time.Time
}

func novoRelogio() *relogio {
	return &relogio{t: time.Date(2026, 10, 18, 14, 0, 0, 0, time.UTC)}
}

func (r *relogio) now() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.t
}

func (r *relogio) avanca(d time.Duration) {
	r.mu.Lock()
	r.t = r.t.Add(d)
	r.mu.Unlock()
}

func janela(pct float64, reset int64) *Window {
	return &Window{UsedPercentage: pct, ResetsAt: reset}
}

// recebeu informa, sem bloquear, se o canal tem sinal pendente.
func recebeu(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

func carregar(t *testing.T, path string, names []string, r *relogio) *Tracker {
	t.Helper()
	tr, err := Load(path, names, r.now)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return tr
}

func lerEstado(t *testing.T, path string) map[string]savedEntry {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("lendo estado: %v", err)
	}
	var m map[string]savedEntry
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("estado gravado não é JSON válido: %v", err)
	}
	return m
}

func TestApplyJanelaAusenteMantemAnterior(t *testing.T) {
	casos := []struct {
		nome     string
		segunda  Reading
		fiveHour *Window
		sevenDay *Window
	}{
		{
			nome:     "só a de 5 h chega",
			segunda:  Reading{FiveHour: janela(50, 2000)},
			fiveHour: janela(50, 2000),
			sevenDay: janela(10, 9000),
		},
		{
			nome:     "só a semanal chega",
			segunda:  Reading{SevenDay: janela(11, 9000)},
			fiveHour: janela(30, 1000),
			sevenDay: janela(11, 9000),
		},
		{
			nome:     "as duas chegam",
			segunda:  Reading{FiveHour: janela(1, 3000), SevenDay: janela(2, 9500)},
			fiveHour: janela(1, 3000),
			sevenDay: janela(2, 9500),
		},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			r := novoRelogio()
			tr := carregar(t, "", nil, r)
			tr.Apply("pessoal", Reading{FiveHour: janela(30, 1000), SevenDay: janela(10, 9000)})
			r.avanca(5 * time.Second)
			tr.Apply("pessoal", c.segunda)

			snap := tr.Snapshot()
			if len(snap) != 1 {
				t.Fatalf("Snapshot tem %d entradas, quer 1", len(snap))
			}
			e := snap[0]
			if !reflect.DeepEqual(e.FiveHour, c.fiveHour) || !reflect.DeepEqual(e.SevenDay, c.sevenDay) {
				t.Errorf("janelas = %+v / %+v, quer %+v / %+v", e.FiveHour, e.SevenDay, c.fiveHour, c.sevenDay)
			}
			if !e.ReceivedAt.Equal(r.now()) {
				t.Errorf("ReceivedAt = %v, quer %v", e.ReceivedAt, r.now())
			}
		})
	}
}

func TestApplySemJanelaNaoMuda(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "uso-plano.json")
	r := novoRelogio()
	tr := carregar(t, path, nil, r)
	ch := tr.Subscribe()

	tr.Apply("", Reading{FiveHour: janela(30, 1000)})
	recebeu(ch)
	antes := tr.Snapshot()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	r.avanca(2 * time.Minute) // mesmo depois do minuto, leitura vazia não avisa
	casos := []Reading{
		{},
		{TranscriptPath: "/x/y.jsonl"},
	}
	for _, rd := range casos {
		if tr.Apply("", rd) {
			t.Errorf("Apply(%+v) = true, quer false", rd)
		}
		if tr.Apply("nova", rd) {
			t.Errorf("Apply em conta nova sem janela = true, quer false")
		}
	}
	if recebeu(ch) {
		t.Error("leitura sem janela avisou os assinantes")
	}
	if depois := tr.Snapshot(); !reflect.DeepEqual(antes, depois) {
		t.Errorf("estado mudou: antes %+v, depois %+v", antes, depois)
	}
	info2, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info2.ModTime().Equal(info.ModTime()) {
		t.Error("leitura sem janela regravou o arquivo")
	}
}

func TestApplyAvisaSoNaMudancaOuDepoisDeUmMinuto(t *testing.T) {
	type passo struct {
		avanca  time.Duration
		conta   string
		leitura Reading
		avisa   bool
	}
	base := Reading{FiveHour: janela(30, 1000), SevenDay: janela(10, 9000)}
	casos := []struct {
		nome   string
		passos []passo
	}{
		{
			nome: "primeira leitura avisa, repetição no minuto não",
			passos: []passo{
				{0, "", base, true},
				{10 * time.Second, "", base, false},
				{40 * time.Second, "", base, false},
			},
		},
		{
			nome: "repetição depois de um minuto avisa",
			passos: []passo{
				{0, "", base, true},
				{59 * time.Second, "", base, false},
				{time.Second, "", base, true},
				{30 * time.Second, "", base, false},
			},
		},
		{
			nome: "percentual muda avisa na hora",
			passos: []passo{
				{0, "", base, true},
				{time.Second, "", Reading{FiveHour: janela(31, 1000)}, true},
				{time.Second, "", Reading{FiveHour: janela(31, 1000)}, false},
			},
		},
		{
			nome: "resets_at muda avisa na hora",
			passos: []passo{
				{0, "", base, true},
				{time.Second, "", Reading{SevenDay: janela(10, 9001)}, true},
			},
		},
		{
			nome: "janela nova que antes faltava avisa",
			passos: []passo{
				{0, "", Reading{FiveHour: janela(30, 1000)}, true},
				{time.Second, "", base, true},
			},
		},
		{
			nome: "o minuto conta por conta",
			passos: []passo{
				{0, "a", base, true},
				{time.Second, "b", base, true},
				{time.Second, "a", base, false},
				{58 * time.Second, "a", base, true},
				{time.Second, "b", base, true},
			},
		},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "uso-plano.json")
			r := novoRelogio()
			tr := carregar(t, path, nil, r)
			ch := tr.Subscribe()
			var gravadoEm map[string]time.Time
			for i, p := range c.passos {
				r.avanca(p.avanca)
				got := tr.Apply(p.conta, p.leitura)
				if got != p.avisa {
					t.Errorf("passo %d: Apply = %v, quer %v", i, got, p.avisa)
				}
				if s := recebeu(ch); s != p.avisa {
					t.Errorf("passo %d: sinal = %v, quer %v", i, s, p.avisa)
				}
				// O arquivo só muda quando avisa.
				atual := map[string]time.Time{}
				if _, err := os.Stat(path); err == nil {
					for k, v := range lerEstado(t, path) {
						atual[k] = v.ReceivedAt
					}
				}
				if gravou := !reflect.DeepEqual(atual, gravadoEm); gravou != p.avisa {
					t.Errorf("passo %d: gravou = %v, quer %v", i, gravou, p.avisa)
				}
				gravadoEm = atual
			}
		})
	}
}

func TestLoadFormatoAntigoViraContaImplicita(t *testing.T) {
	quando := time.Date(2026, 10, 18, 14, 2, 11, 0, time.FixedZone("", -3*3600))
	casos := []struct {
		nome    string
		arquivo string
		names   []string
		quer    []Entry
	}{
		{
			nome:    "formato antigo",
			arquivo: `{"five_hour":{"used_percentage":37,"resets_at":1792171200},"seven_day":{"used_percentage":12,"resets_at":1792603200},"received_at":"2026-10-18T14:02:11-03:00"}`,
			quer: []Entry{{
				Account:    "",
				FiveHour:   janela(37, 1792171200),
				SevenDay:   janela(12, 1792603200),
				ReceivedAt: quando,
			}},
		},
		{
			nome:    "formato antigo sem -account vira a conta implícita declarada",
			arquivo: `{"seven_day":{"used_percentage":12,"resets_at":1792603200},"received_at":"2026-10-18T14:02:11-03:00"}`,
			names:   []string{""},
			quer:    []Entry{{Account: "", SevenDay: janela(12, 1792603200), ReceivedAt: quando}},
		},
		{
			nome:    "formato antigo com contas declaradas é ignorado",
			arquivo: `{"seven_day":{"used_percentage":12,"resets_at":1792603200},"received_at":"2026-10-18T14:02:11-03:00"}`,
			names:   []string{"pessoal"},
			quer:    []Entry{{Account: "pessoal"}},
		},
		{
			nome: "conta que saiu das flags não volta",
			arquivo: `{
				"velha":   {"seven_day":{"used_percentage":90,"resets_at":1792603200},"received_at":"2026-10-18T14:02:11-03:00"},
				"pessoal": {"seven_day":{"used_percentage":12,"resets_at":1792603200},"received_at":"2026-10-18T14:02:11-03:00"}
			}`,
			names: []string{"pessoal"},
			quer:  []Entry{{Account: "pessoal", SevenDay: janela(12, 1792603200), ReceivedAt: quando}},
		},
		{
			nome: "mapa por conta, janela inválida descartada",
			arquivo: `{
				"trabalho": {"seven_day":{"used_percentage":64.5,"resets_at":1792603200},"received_at":"2026-10-18T14:02:11-03:00"},
				"pessoal":  {"five_hour":{"used_percentage":150,"resets_at":1792171200},"seven_day":{"used_percentage":12,"resets_at":0},"received_at":"2026-10-18T14:02:11-03:00"}
			}`,
			quer: []Entry{
				{Account: "pessoal"},
				{Account: "trabalho", SevenDay: janela(64.5, 1792603200), ReceivedAt: quando},
			},
		},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "uso-plano.json")
			if err := os.WriteFile(path, []byte(c.arquivo), 0o600); err != nil {
				t.Fatal(err)
			}
			tr := carregar(t, path, c.names, novoRelogio())
			got := tr.Snapshot()
			if len(got) != len(c.quer) {
				t.Fatalf("Snapshot = %+v, quer %+v", got, c.quer)
			}
			for i := range got {
				g, q := got[i], c.quer[i]
				if g.Account != q.Account || !reflect.DeepEqual(g.FiveHour, q.FiveHour) ||
					!reflect.DeepEqual(g.SevenDay, q.SevenDay) || !g.ReceivedAt.Equal(q.ReceivedAt) {
					t.Errorf("entrada %d = %+v, quer %+v", i, g, q)
				}
			}
		})
	}
}

func TestLoadArquivoCorrompidoComecaVazio(t *testing.T) {
	casos := []struct {
		nome    string
		prepara func(t *testing.T, path string)
		erro    bool
	}{
		{"ausente", func(t *testing.T, path string) {}, false},
		{"JSON truncado", func(t *testing.T, path string) {
			os.WriteFile(path, []byte(`{"pessoal": {"five_hour": {"used_perc`), 0o600)
		}, true},
		{"não é objeto", func(t *testing.T, path string) {
			os.WriteFile(path, []byte(`[1,2,3]`), 0o600)
		}, true},
		{"conta com tipo errado", func(t *testing.T, path string) {
			os.WriteFile(path, []byte(`{"pessoal": "segredo"}`), 0o600)
		}, true},
		{"ilegível (é diretório)", func(t *testing.T, path string) {
			os.Mkdir(path, 0o700)
		}, true},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "uso-plano.json")
			c.prepara(t, path)
			tr, err := Load(path, []string{"pessoal"}, novoRelogio().now)
			if (err != nil) != c.erro {
				t.Fatalf("erro = %v, quer erro: %v", err, c.erro)
			}
			if err != nil && strings.Contains(err.Error(), "segredo") {
				t.Errorf("erro cita o conteúdo do arquivo: %v", err)
			}
			if tr == nil {
				t.Fatal("Load devolveu Tracker nil")
			}
			snap := tr.Snapshot()
			if len(snap) != 1 || snap[0].Account != "pessoal" || snap[0].FiveHour != nil || !snap[0].ReceivedAt.IsZero() {
				t.Errorf("Snapshot = %+v, quer só a conta declarada sem dado", snap)
			}
			// Continua utilizável.
			if !tr.Apply("pessoal", Reading{FiveHour: janela(5, 100)}) {
				t.Error("Apply depois de Load com erro não avisou")
			}
		})
	}
}

func TestGravaComPermissao600(t *testing.T) {
	path := filepath.Join(t.TempDir(), "uso-plano.json")
	r := novoRelogio()
	tr := carregar(t, path, []string{"vazia"}, r)
	tr.Apply("pessoal", Reading{FiveHour: janela(37, 1792171200), SevenDay: janela(12.5, 1792603200)})

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("arquivo não foi gravado: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("permissão = %o, quer 600", perm)
	}
	estado := lerEstado(t, path)
	if _, ok := estado["vazia"]; ok {
		t.Error("conta sem dado foi gravada")
	}
	if e := estado["pessoal"]; !reflect.DeepEqual(e.FiveHour, janela(37, 1792171200)) ||
		!reflect.DeepEqual(e.SevenDay, janela(12.5, 1792603200)) || !e.ReceivedAt.Equal(r.now()) {
		t.Errorf("gravado = %+v", e)
	}

	// Relido ao subir.
	tr2 := carregar(t, path, nil, r)
	if got := tr2.Snapshot(); len(got) != 1 || !reflect.DeepEqual(got[0].FiveHour, janela(37, 1792171200)) {
		t.Errorf("releitura = %+v", got)
	}
}

func TestGravaFalhaSegueEmMemoria(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inexistente", "uso-plano.json")
	r := novoRelogio()
	tr := carregar(t, path, nil, r)
	if !tr.Apply("", Reading{FiveHour: janela(1, 100)}) {
		t.Fatal("Apply não avisou")
	}
	if got := tr.Snapshot(); len(got) != 1 || got[0].FiveHour == nil {
		t.Errorf("estado em memória perdido: %+v", got)
	}
}

func TestContasDeclaradasNascemSemDado(t *testing.T) {
	casos := []struct {
		nome  string
		names []string
		quer  []string
	}{
		{"nenhuma", nil, []string{}},
		{"uma", []string{"pessoal"}, []string{"pessoal"}},
		{"ordenadas", []string{"trabalho", "pessoal", "alfa"}, []string{"alfa", "pessoal", "trabalho"}},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			tr := carregar(t, "", c.names, novoRelogio())
			snap := tr.Snapshot()
			if snap == nil {
				t.Fatal("Snapshot devolveu nil")
			}
			got := []string{}
			for _, e := range snap {
				got = append(got, e.Account)
				if e.FiveHour != nil || e.SevenDay != nil || !e.ReceivedAt.IsZero() {
					t.Errorf("conta %q nasceu com dado: %+v", e.Account, e)
				}
			}
			if !reflect.DeepEqual(got, c.quer) {
				t.Errorf("contas = %v, quer %v", got, c.quer)
			}
			b, _ := json.Marshal(snap)
			if len(c.names) == 0 && string(b) != "[]" {
				t.Errorf("JSON = %s, quer []", b)
			}
			if len(c.names) == 1 && string(b) != `[{"account":"pessoal"}]` {
				t.Errorf("JSON = %s", b)
			}
		})
	}
}

func TestSnapshotECopia(t *testing.T) {
	tr := carregar(t, "", nil, novoRelogio())
	tr.Apply("pessoal", Reading{FiveHour: janela(30, 1000), SevenDay: janela(10, 9000)})

	snap := tr.Snapshot()
	snap[0].FiveHour.UsedPercentage = 99
	snap[0].SevenDay.ResetsAt = 1
	snap[0].Account = "mexida"
	snap[0].ReceivedAt = time.Time{}

	novo := tr.Snapshot()
	if novo[0].Account != "pessoal" || novo[0].FiveHour.UsedPercentage != 30 ||
		novo[0].SevenDay.ResetsAt != 9000 || novo[0].ReceivedAt.IsZero() {
		t.Errorf("mexer no retorno mudou o estado: %+v", novo[0])
	}

	// A leitura passada a Apply também não fica compartilhada.
	w := janela(40, 2000)
	tr.Apply("pessoal", Reading{FiveHour: w})
	w.UsedPercentage = 77
	if got := tr.Snapshot()[0].FiveHour.UsedPercentage; got != 40 {
		t.Errorf("Apply guardou o ponteiro da leitura: %v", got)
	}
}

func TestApplySnapshotConcorrentes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "uso-plano.json")
	tr := carregar(t, path, []string{"a", "b"}, novoRelogio())
	ch := tr.Subscribe()
	var wg sync.WaitGroup
	for g := range 4 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for i := range 200 {
				conta := []string{"a", "b"}[g%2]
				tr.Apply(conta, Reading{FiveHour: janela(float64(i%100), int64(1000+i))})
			}
		}()
		go func() {
			defer wg.Done()
			for range 200 {
				for _, e := range tr.Snapshot() {
					if e.FiveHour != nil {
						_ = e.FiveHour.UsedPercentage
					}
				}
				recebeu(ch)
			}
		}()
	}
	wg.Wait()
	if got := len(tr.Snapshot()); got != 2 {
		t.Errorf("Snapshot tem %d contas, quer 2", got)
	}
}
