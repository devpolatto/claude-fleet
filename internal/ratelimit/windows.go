package ratelimit

import (
	"context"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/devpolatto/claude-fleet/internal/pricing"
	"github.com/devpolatto/claude-fleet/internal/transcript"
)

// DefaultWindowsInterval é o intervalo padrão entre voltas do Windows. As
// janelas mudam devagar, e cada volta percorre os diretórios inteiros.
const DefaultWindowsInterval = 30 * time.Second

// Duração das janelas do plano.
const (
	fiveHourSpan = 5 * time.Hour
	sevenDaySpan = 7 * 24 * time.Hour
)

// WindowSum é a soma de tokens e o custo estimado (AD-005) de uma janela.
// CostKnown é falso quando algum modelo com tokens na janela não tem preço;
// CostUSD soma então só os modelos conhecidos.
type WindowSum struct {
	Tokens    pricing.Tokens `json:"tokens"`
	CostUSD   float64        `json:"cost_usd"`
	CostKnown bool           `json:"cost_known"`
}

// WindowTotals são as somas das janelas de 5 h e de 7 dias de uma conta.
type WindowTotals struct {
	FiveHour WindowSum `json:"five_hour"`
	SevenDay WindowSum `json:"seven_day"`
}

// Windows soma, por conta, os tokens e o custo das janelas de 5 h e 7 dias a
// partir dos transcripts em disco (USO-09) e entrega o resultado ao Tracker.
//
// Cada volta percorre o diretório projects de cada conta atrás de *.jsonl
// (inclusive <sessão>/subagents/*.jsonl) modificados nos últimos 7 dias. Cada
// arquivo tem um transcript.File mantido entre voltas, de modo que só o que
// foi acrescentado é lido; arquivos que saem da janela ou somem são soltos.
// A soma usa a deduplicação por message.id do transcript (L-001).
//
// Nenhum caminho nem conteúdo de transcript vai para o log (AD-003).
type Windows struct {
	dirs map[string]string // conta → diretório projects
	tr   *Tracker

	mu    sync.Mutex // serializa RefreshOnce
	files map[string]*transcript.File
}

// NewWindows cria o Windows para as contas em dirs (nome → diretório
// projects da conta). Só essas contas recebem janelas.
func NewWindows(dirs map[string]string, tr *Tracker) *Windows {
	d := make(map[string]string, len(dirs))
	for k, v := range dirs {
		d[k] = v
	}
	return &Windows{dirs: d, tr: tr, files: make(map[string]*transcript.File)}
}

// Run chama RefreshOnce logo de início e depois a cada interval, até o
// contexto ser cancelado. interval ≤ 0 usa DefaultWindowsInterval.
func (w *Windows) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = DefaultWindowsInterval
	}
	tk := time.NewTicker(interval)
	defer tk.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		w.RefreshOnce(time.Now())
		select {
		case <-ctx.Done():
			return
		case <-tk.C:
		}
	}
}

// RefreshOnce faz uma volta com o horário now e grava as janelas de cada
// conta com Tracker.SetWindows.
//
// O início da janela de 5 h é five_hour.resets_at − 5 h quando a conta tem
// esse dado e o resets_at ainda está no futuro (a janela do plano em curso);
// senão, now − 5 h. A janela de 7 dias segue a mesma regra com
// seven_day.resets_at − 7 dias, para os números baterem com o medidor
// semanal; sem dado válido, now − 7 dias.
func (w *Windows) RefreshOnce(now time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()

	accounts := make([]string, 0, len(w.dirs))
	for a := range w.dirs {
		accounts = append(accounts, a)
	}
	sort.Strings(accounts)

	seen := make(map[string]bool)
	for _, acc := range accounts {
		files := w.scan(w.dirs[acc], now.Add(-sevenDaySpan), seen)
		planFive, planSeven := w.tr.planStarts(acc, now)
		fiveStart := windowStart(planFive, now, fiveHourSpan)
		sevenStart := windowStart(planSeven, now, sevenDaySpan)

		// Uma sessão retomada pode repetir, num arquivo novo, mensagens de
		// outro com o mesmo message.id: deduplica por conta, ficando com a
		// cópia de timestamp mais recente (como a L-001 dentro de um arquivo).
		// Logo depois do reset semanal, a semana pode começar depois do
		// início da janela de 5 h: busca a partir do mais antigo dos dois.
		since := sevenStart
		if fiveStart.Before(since) {
			since = fiveStart
		}
		byID := make(map[string]transcript.Call)
		var anon []transcript.Call
		for _, f := range files {
			for _, c := range f.CallsSince(since) {
				if c.ID == "" {
					anon = append(anon, c)
				} else if old, ok := byID[c.ID]; !ok || c.TS.After(old.TS) {
					byID[c.ID] = c
				}
			}
		}
		five := make(map[transcript.ModelKey]transcript.Tokens)
		seven := make(map[transcript.ModelKey]transcript.Tokens)
		add := func(c transcript.Call) {
			if !c.TS.Before(sevenStart) {
				seven[c.Key] = seven[c.Key].Add(c.Tokens)
			}
			if !c.TS.Before(fiveStart) {
				five[c.Key] = five[c.Key].Add(c.Tokens)
			}
		}
		for _, c := range byID {
			add(c)
		}
		for _, c := range anon {
			add(c)
		}
		w.tr.SetWindows(acc, WindowTotals{FiveHour: sumOf(five), SevenDay: sumOf(seven)})
	}

	// Arquivos que saíram da janela, sumiram ou deixaram de ser lidos.
	for p := range w.files {
		if !seen[p] {
			delete(w.files, p)
		}
	}
}

// windowStart devolve o início da janela pelo plano, ou now − span quando o
// plano não dá um início entre now − span e now.
func windowStart(plan, now time.Time, span time.Duration) time.Time {
	if plan.IsZero() || plan.After(now) || plan.Before(now.Add(-span)) {
		return now.Add(-span)
	}
	return plan
}

// scan percorre dir atrás de *.jsonl com mtime em since ou depois, atualiza
// o leitor de cada um e devolve os leitores lidos com sucesso. Os caminhos
// lidos são marcados em seen. Diretório inexistente ou ilegível não é erro:
// a conta fica com as janelas zeradas.
func (w *Windows) scan(dir string, since time.Time, seen map[string]bool) []*transcript.File {
	if dir == "" {
		return nil
	}
	// WalkDir não segue links simbólicos; o diretório da conta pode ser um.
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	var out []*transcript.File
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// Raiz ausente ou subdiretório ilegível: pula sem log, para não
			// citar caminhos.
			if d != nil && d.IsDir() && path != dir {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() || !d.Type().IsRegular() || !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.ModTime().Before(since) {
			return nil
		}
		f, ok := w.files[path]
		if !ok {
			f = transcript.New(path)
			w.files[path] = f
		}
		if f.Refresh() != nil {
			return nil // sumiu entre a listagem e a abertura: sai do cache
		}
		seen[path] = true
		out = append(out, f)
		return nil
	})
	return out
}

// sumOf soma os tokens e calcula o custo com pricing.Cost, como o
// store.Refresher: modelos com tokens zerados (ex.: "<synthetic>") são
// ignorados, e CostKnown só é verdadeiro se todos os demais tiverem preço.
// As chaves são percorridas em ordem fixa, para o custo não oscilar no
// último bit e avisar à toa.
func sumOf(byModel map[transcript.ModelKey]transcript.Tokens) WindowSum {
	keys := make([]transcript.ModelKey, 0, len(byModel))
	for k := range byModel {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Model != keys[j].Model {
			return keys[i].Model < keys[j].Model
		}
		return !keys[i].Fast && keys[j].Fast
	})

	out := WindowSum{CostKnown: true}
	var total transcript.Tokens
	for _, k := range keys {
		tok := byModel[k]
		if tok.IsZero() {
			continue
		}
		total = total.Add(tok)
		usd, known := pricing.Cost(k.Model, pricing.Tokens(tok), k.Fast)
		out.CostUSD += usd
		out.CostKnown = out.CostKnown && known
	}
	out.Tokens = pricing.Tokens(total)
	return out
}
