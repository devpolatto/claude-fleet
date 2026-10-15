package store

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/devpolatto/claude-fleet/internal/pricing"
	"github.com/devpolatto/claude-fleet/internal/transcript"
)

// DefaultRefreshInterval é o intervalo padrão entre passadas do Refresher.
const DefaultRefreshInterval = 5 * time.Second

// AgentUsage é o consumo de um subagente lido do disco.
type AgentUsage struct {
	Usage                             Usage
	Models                            map[string]Usage
	Type, Description, TranscriptPath string
	// FirstAt e LastAt são o primeiro e o último timestamp do transcript.
	// Só são usados ao criar um subagente que apareceu no disco sem evento.
	FirstAt, LastAt time.Time
}

// SessionUsage é o consumo de uma sessão lido do disco pelo Refresher.
type SessionUsage struct {
	Main              Usage            // só a sessão principal
	Models            map[string]Usage // da sessão principal, por nome de modelo
	TranscriptMissing bool             // transcript principal ilegível ou ausente
	ParseErrors       int              // linhas ilegíveis no principal e nos subagentes
	Agents            map[string]AgentUsage
}

// SetUsage grava o consumo lido do disco na sessão e informa se algo mudou;
// só nesse caso os assinantes são notificados. Sessão desconhecida devolve
// false.
//
// Com TranscriptMissing, Main e Models não são tocados: os totais anteriores
// ficam. Subagente ainda desconhecido é criado como concluido, com Type e
// Description do .meta.json. Em subagente já conhecido o status não muda;
// Type, Description e TranscriptPath só são preenchidos se estiverem vazios.
func (s *Store) SetUsage(sessionID string, u SessionUsage) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	sess, ok := s.sessions[sessionID]
	if !ok {
		return false
	}
	m := &mutation{}
	if sess.TranscriptMissing != u.TranscriptMissing {
		sess.TranscriptMissing = u.TranscriptMissing
		m.changed = true
	}
	if sess.ParseErrors != u.ParseErrors {
		sess.ParseErrors = u.ParseErrors
		m.changed = true
	}
	if !u.TranscriptMissing {
		m.setUsage(&sess.Main, u.Main)
		m.setModels(&sess.Models, u.Models)
	}

	for id, au := range u.Agents {
		ag := sess.Subagents[id]
		if ag == nil {
			ag = &Agent{
				ID:          id,
				Type:        au.Type,
				Description: au.Description,
				Status:      StatusConcluido,
				StartedAt:   au.FirstAt,
				LastEventAt: au.LastAt,
			}
			if !au.LastAt.IsZero() {
				t := au.LastAt
				ag.EndedAt = &t
			}
			if sess.Subagents == nil {
				sess.Subagents = make(map[string]*Agent)
			}
			sess.Subagents[id] = ag
			m.changed = true
		}
		if ag.Type == "" {
			m.setString(&ag.Type, au.Type)
		}
		if ag.Description == "" {
			m.setString(&ag.Description, au.Description)
		}
		if ag.TranscriptPath == "" {
			m.setString(&ag.TranscriptPath, au.TranscriptPath)
		}
		m.setUsage(&ag.Usage, au.Usage)
		m.setModels(&ag.Models, au.Models)
	}

	if m.changed {
		s.notify()
	}
	return m.changed
}

func (m *mutation) setUsage(p *Usage, v Usage) {
	if *p != v {
		*p = v
		m.changed = true
	}
}

// setModels troca o mapa se o conteúdo mudou; nil e vazio são iguais.
func (m *mutation) setModels(p *map[string]Usage, v map[string]Usage) {
	if modelsEqual(*p, v) {
		return
	}
	*p = copyModels(v)
	m.changed = true
}

func modelsEqual(a, b map[string]Usage) bool {
	if len(a) != len(b) {
		return false
	}
	for k, va := range a {
		if vb, ok := b[k]; !ok || va != vb {
			return false
		}
	}
	return true
}

// refreshTarget é o mínimo que o Refresher precisa saber de uma sessão.
type refreshTarget struct {
	id, transcriptPath string
	ended              bool
}

// refreshTargets lista as sessões sob o lock de leitura, para que o disco
// seja lido depois, sem o lock.
func (s *Store) refreshTargets() []refreshTarget {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]refreshTarget, 0, len(s.sessions))
	for _, sess := range s.sessions {
		out = append(out, refreshTarget{
			id:             sess.ID,
			transcriptPath: sess.TranscriptPath,
			ended:          sess.Status == StatusEncerrado,
		})
	}
	return out
}

// Refresher lê periodicamente os transcripts das sessões e grava tokens e
// custo no Store. Os leitores ficam em cache por caminho, de modo que cada
// passada só lê o que foi acrescentado ao arquivo.
type Refresher struct {
	store *Store

	mu        sync.Mutex // serializa RefreshOnce
	files     map[string]*transcript.File
	paths     map[string][]string // sessão → caminhos em cache
	finalDone map[string]bool     // sessões encerradas que já tiveram a última leitura
}

// NewRefresher cria um Refresher para o Store dado.
func NewRefresher(s *Store) *Refresher {
	return &Refresher{
		store:     s,
		files:     make(map[string]*transcript.File),
		paths:     make(map[string][]string),
		finalDone: make(map[string]bool),
	}
}

// Run chama RefreshOnce logo de início e depois a cada interval, até o
// contexto ser cancelado. interval ≤ 0 usa DefaultRefreshInterval.
func (r *Refresher) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = DefaultRefreshInterval
	}
	tk := time.NewTicker(interval)
	defer tk.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		r.RefreshOnce()
		select {
		case <-ctx.Done():
			return
		case <-tk.C:
		}
	}
}

// RefreshOnce faz uma passada: lê os transcripts das sessões não encerradas
// e, uma única vez, os das que acabaram de encerrar. A leitura do disco
// acontece fora do lock do Store; o resultado entra com SetUsage.
func (r *Refresher) RefreshOnce() {
	r.mu.Lock()
	defer r.mu.Unlock()

	targets := r.store.refreshTargets()
	alive := make(map[string]bool, len(targets))
	for _, t := range targets {
		alive[t.id] = true
		if !t.ended {
			// Sessão reaberta volta a ser lida a cada passada.
			delete(r.finalDone, t.id)
		} else if r.finalDone[t.id] {
			continue
		}
		if t.transcriptPath != "" {
			r.store.SetUsage(t.id, r.read(t.id, t.transcriptPath))
		}
		if t.ended {
			// Última leitura feita: os leitores não serão mais usados.
			r.finalDone[t.id] = true
			r.drop(t.id)
		}
	}

	// Sessões removidas do Store (Prune) saem do cache.
	for id := range r.paths {
		if !alive[id] {
			r.drop(id)
		}
	}
	for id := range r.finalDone {
		if !alive[id] {
			delete(r.finalDone, id)
		}
	}
}

// read lê o transcript principal e os dos subagentes da sessão.
func (r *Refresher) read(sessionID, mainPath string) SessionUsage {
	var u SessionUsage
	used := []string{mainPath}

	main := r.file(mainPath)
	if err := main.Refresh(); err != nil {
		// Inexistente (ou ilegível): sinaliza e mantém os totais anteriores.
		u.TranscriptMissing = true
	} else {
		u.Main, u.Models = usageOf(main.ByModel())
	}
	u.ParseErrors = main.ParseErrors()

	for _, sf := range transcript.SubagentsOf(mainPath) {
		f := r.file(sf.TranscriptPath)
		used = append(used, sf.TranscriptPath)
		if err := f.Refresh(); err != nil {
			continue // some entre o ReadDir e a abertura: fica como estava
		}
		u.ParseErrors += f.ParseErrors()
		au := AgentUsage{
			Type:           sf.Type,
			Description:    sf.Description,
			TranscriptPath: sf.TranscriptPath,
			FirstAt:        f.FirstTS(),
			LastAt:         f.LastTS(),
		}
		au.Usage, au.Models = usageOf(f.ByModel())
		if u.Agents == nil {
			u.Agents = make(map[string]AgentUsage)
		}
		u.Agents[sf.AgentID] = au
	}

	// Caminhos que a sessão deixou de usar (ex.: transcript trocado num
	// --resume) saem do cache.
	keep := make(map[string]bool, len(used))
	for _, p := range used {
		keep[p] = true
	}
	for _, p := range r.paths[sessionID] {
		if !keep[p] {
			delete(r.files, p)
		}
	}
	r.paths[sessionID] = used
	return u
}

// file devolve o leitor em cache para path, criando-o se preciso.
func (r *Refresher) file(path string) *transcript.File {
	f, ok := r.files[path]
	if !ok {
		f = transcript.New(path)
		r.files[path] = f
	}
	return f
}

// drop tira do cache os leitores da sessão.
func (r *Refresher) drop(sessionID string) {
	for _, p := range r.paths[sessionID] {
		delete(r.files, p)
	}
	delete(r.paths, sessionID)
}

// usageOf calcula o total e o consumo por nome de modelo (fast e padrão
// somados). CostKnown só é verdadeiro se todos os modelos tiverem preço;
// modelos com tokens zerados (ex.: "<synthetic>") são ignorados, porque não
// custam nada e não devem tornar o custo "n/d".
func usageOf(byModel map[transcript.ModelKey]transcript.Tokens) (Usage, map[string]Usage) {
	// Ordem fixa: a soma em ponto flutuante depende da ordem, e uma ordem
	// aleatória faria o custo oscilar no último bit e notificar à toa.
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

	total := Usage{CostKnown: true}
	var models map[string]Usage
	for _, key := range keys {
		tok := byModel[key]
		if tok.IsZero() {
			continue
		}
		pt := pricing.Tokens(tok)
		usd, known := pricing.Cost(key.Model, pt, key.Fast)
		mu := Usage{Tokens: pt, CostUSD: usd, CostKnown: known}
		total = total.add(mu)
		if models == nil {
			models = make(map[string]Usage)
		}
		if prev, ok := models[key.Model]; ok {
			mu = prev.add(mu)
		}
		models[key.Model] = mu
	}
	return total, models
}
