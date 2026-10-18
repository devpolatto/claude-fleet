package ratelimit

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/devpolatto/claude-fleet/internal/atomicfile"
)

// staleAfter é o intervalo depois do qual uma leitura repetida (mesmos
// valores) volta a avisar e gravar, para o "atualizado há" da página ficar
// certo com resolução de minuto. Também limita o log de falha ao gravar.
const staleAfter = time.Minute

// filePerm é a permissão do arquivo de estado.
const filePerm fs.FileMode = 0o600

// Entry é o último uso do plano conhecido de uma conta.
type Entry struct {
	Account    string    `json:"account"` // "" sem -account
	FiveHour   *Window   `json:"five_hour,omitempty"`
	SevenDay   *Window   `json:"seven_day,omitempty"`
	ReceivedAt time.Time `json:"received_at,omitzero"` // zero: nunca chegou dado
	// Windows são os tokens e o custo das janelas, somados dos transcripts
	// (P2). Não vão para o arquivo: são recalculados ao subir.
	Windows *WindowTotals `json:"windows,omitempty"`
}

// fileEntry é o valor de cada conta no arquivo. As janelas são lidas como
// rawWindow para passar pela mesma validação do Parse.
type fileEntry struct {
	FiveHour   *rawWindow `json:"five_hour,omitempty"`
	SevenDay   *rawWindow `json:"seven_day,omitempty"`
	ReceivedAt time.Time  `json:"received_at,omitzero"`
}

// savedEntry é o formato gravado (janelas já validadas).
type savedEntry struct {
	FiveHour   *Window   `json:"five_hour,omitempty"`
	SevenDay   *Window   `json:"seven_day,omitempty"`
	ReceivedAt time.Time `json:"received_at,omitzero"`
}

// Tracker guarda o último uso do plano por conta, avisa os assinantes quando
// ele muda e o persiste em disco. É seguro para uso concorrente.
//
// O ReceivedAt em memória acompanha toda leitura com ao menos uma janela,
// mesmo quando ela não gera aviso; o aviso (e a gravação) sai no máximo a
// cada staleAfter em leituras repetidas, então quem lê o estado no aviso vê
// um ReceivedAt com no máximo um minuto de atraso, e quem chega depois (uma
// conexão nova) vê o mais recente.
type Tracker struct {
	path string // "" = só memória
	now  func() time.Time

	mu         sync.Mutex
	entries    map[string]*Entry
	lastNotice map[string]time.Time // último aviso por conta
	lastLogErr time.Time            // último log de falha ao gravar
	subs       []chan struct{}
}

// Load cria o Tracker a partir do arquivo em path. Arquivo ausente começa
// vazio, sem erro. Cada nome em names ganha uma entrada sem dado, para a
// tela mostrar "sem dado" naquela conta. path vazio: só memória. now nil
// usa time.Now.
//
// Arquivo ilegível ou corrompido: Load devolve um Tracker vazio e utilizável
// junto com o erro, para quem chama logar e seguir. O erro cita só o
// caminho e a posição estrutural do problema, nunca o conteúdo. O Tracker
// continua tentando gravar em path, de modo que a próxima leitura válida
// substitui um arquivo corrompido.
//
// O formato antigo (um objeto com five_hour, seven_day ou received_at no
// topo, sem conta) vira a conta "". Janelas inválidas no arquivo são
// descartadas como ausentes.
func Load(path string, names []string, now func() time.Time) (*Tracker, error) {
	if now == nil {
		now = time.Now
	}
	t := &Tracker{
		path:       path,
		now:        now,
		entries:    make(map[string]*Entry),
		lastNotice: make(map[string]time.Time),
	}
	for _, n := range names {
		t.entry(n)
	}
	if path == "" {
		return t, nil
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return t, nil
	}
	if err != nil {
		return t, fmt.Errorf("ratelimit: lendo o estado: %w", err)
	}
	saved, err := decodeFile(data)
	if err != nil {
		return t, fmt.Errorf("ratelimit: estado em %s ignorado: %w", path, err)
	}
	for name, fe := range saved {
		e := t.entry(name)
		e.FiveHour = fe.FiveHour.valid()
		e.SevenDay = fe.SevenDay.valid()
		if e.FiveHour != nil || e.SevenDay != nil {
			e.ReceivedAt = fe.ReceivedAt
		}
	}
	return t, nil
}

// decodeFile lê o mapa por conta, ou o formato antigo de valor único.
func decodeFile(data []byte) (map[string]fileEntry, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, describeJSONError(err)
	}
	_, a := top["five_hour"]
	_, b := top["seven_day"]
	_, c := top["received_at"]
	if a || b || c {
		var fe fileEntry
		if err := json.Unmarshal(data, &fe); err != nil {
			return nil, describeJSONError(err)
		}
		return map[string]fileEntry{"": fe}, nil
	}
	out := make(map[string]fileEntry, len(top))
	for name, raw := range top {
		var fe fileEntry
		if err := json.Unmarshal(raw, &fe); err != nil {
			return nil, describeJSONError(err)
		}
		out[name] = fe
	}
	return out, nil
}

// entry devolve a entrada da conta, criando-a vazia. Exige t.mu travado
// (ou Tracker ainda não publicado).
func (t *Tracker) entry(name string) *Entry {
	e, ok := t.entries[name]
	if !ok {
		e = &Entry{Account: name}
		t.entries[name] = e
	}
	return e
}

// Subscribe devolve um canal que recebe um sinal a cada aviso. O canal tem
// buffer 1 e o envio não bloqueia: sinais seguidos enquanto o assinante não
// lê se fundem num só, e um assinante lento nunca trava Apply. Quem recebe
// o sinal deve ler o estado atual com Snapshot.
func (t *Tracker) Subscribe() <-chan struct{} {
	ch := make(chan struct{}, 1)
	t.mu.Lock()
	t.subs = append(t.subs, ch)
	t.mu.Unlock()
	return ch
}

// notify sinaliza todos os assinantes sem bloquear. Exige t.mu travado.
func (t *Tracker) notify() {
	for _, ch := range t.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// Apply registra a leitura r na conta account. Cada janela presente
// substitui a anterior; janela ausente mantém a anterior. Leitura sem
// nenhuma janela não muda nada, nem o ReceivedAt.
//
// Avisa os assinantes e grava o arquivo quando um percentual ou resets_at
// muda, ou quando o último aviso daquela conta tem staleAfter ou mais.
// changed informa se houve aviso. Falha ao gravar vai para o log no máximo
// uma vez por staleAfter, e o estado segue em memória.
func (t *Tracker) Apply(account string, r Reading) (changed bool) {
	if r.FiveHour == nil && r.SevenDay == nil {
		return false
	}
	now := t.now()

	t.mu.Lock()
	defer t.mu.Unlock()

	e := t.entry(account)
	valueChanged := false
	if r.FiveHour != nil {
		if e.FiveHour == nil || *e.FiveHour != *r.FiveHour {
			valueChanged = true
		}
		w := *r.FiveHour
		e.FiveHour = &w
	}
	if r.SevenDay != nil {
		if e.SevenDay == nil || *e.SevenDay != *r.SevenDay {
			valueChanged = true
		}
		w := *r.SevenDay
		e.SevenDay = &w
	}
	e.ReceivedAt = now

	last, ok := t.lastNotice[account]
	if !valueChanged && ok && now.Sub(last) < staleAfter {
		return false
	}
	t.lastNotice[account] = now
	t.save(now)
	t.notify()
	return true
}

// SetWindows guarda os totais das janelas da conta account, criando a
// entrada se ela não existir, e avisa os assinantes só quando o valor muda.
// Não grava o arquivo: as janelas não fazem parte do uso-plano.json.
func (t *Tracker) SetWindows(account string, w WindowTotals) (changed bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	e := t.entry(account)
	if e.Windows != nil && *e.Windows == w {
		return false
	}
	e.Windows = &w
	t.notify()
	return true
}

// planStarts devolve o início das janelas de 5 h e 7 dias segundo o dado do
// plano da conta: resets_at menos a duração da janela, quando a janela existe
// e o resets_at ainda está no futuro; zero quando não há esse dado.
func (t *Tracker) planStarts(account string, now time.Time) (fiveHour, sevenDay time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	e, ok := t.entries[account]
	if !ok {
		return time.Time{}, time.Time{}
	}
	start := func(w *Window, d time.Duration) time.Time {
		if w == nil {
			return time.Time{}
		}
		reset := time.Unix(w.ResetsAt, 0)
		if !reset.After(now) {
			return time.Time{}
		}
		return reset.Add(-d)
	}
	return start(e.FiveHour, fiveHourSpan), start(e.SevenDay, sevenDaySpan)
}

// save grava o estado em t.path. Exige t.mu travado: as gravações saem na
// ordem dos avisos, e são no máximo uma por minuto por conta em uso
// contínuo, então segurar a trava durante a escrita não pesa.
func (t *Tracker) save(now time.Time) {
	if t.path == "" {
		return
	}
	out := make(map[string]savedEntry, len(t.entries))
	for name, e := range t.entries {
		if e.FiveHour == nil && e.SevenDay == nil {
			continue
		}
		out[name] = savedEntry{FiveHour: e.FiveHour, SevenDay: e.SevenDay, ReceivedAt: e.ReceivedAt}
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err == nil {
		data = append(data, '\n')
		err = atomicfile.Write(t.path, data, filePerm)
	}
	if err != nil && (t.lastLogErr.IsZero() || now.Sub(t.lastLogErr) >= staleAfter) {
		t.lastLogErr = now
		log.Printf("ratelimit: falha ao gravar o uso do plano, seguindo em memória: %v", err)
	}
}

// Snapshot devolve uma cópia profunda das entradas, ordenada por conta.
// Mexer no retorno não muda o estado. Sem contas, devolve lista vazia
// (não nil), que vira [] em JSON.
func (t *Tracker) Snapshot() []Entry {
	t.mu.Lock()
	out := make([]Entry, 0, len(t.entries))
	for _, e := range t.entries {
		c := *e
		if e.FiveHour != nil {
			w := *e.FiveHour
			c.FiveHour = &w
		}
		if e.SevenDay != nil {
			w := *e.SevenDay
			c.SevenDay = &w
		}
		if e.Windows != nil {
			w := *e.Windows
			c.Windows = &w
		}
		out = append(out, c)
	}
	t.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Account < out[j].Account })
	return out
}
