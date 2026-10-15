// Package transcript lê os transcripts JSONL do Claude Code de forma
// incremental, a partir do último offset lido, e soma os tokens gastos.
//
// Cada linha "assistant" traz message.id, message.model e message.usage. O
// mesmo message.id se repete em várias linhas (uma por bloco de conteúdo), por
// isso a soma é deduplicada: o último registro de cada id substitui os
// anteriores (lição L-001).
package transcript

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"sync"
	"time"
)

// Tokens soma os tokens por categoria. Os campos espelham pricing.Tokens, que
// será integrado numa task posterior.
type Tokens struct {
	Input, Cache5m, Cache1h, CacheRead, Output int64
}

// Add devolve a soma campo a campo de t e o.
func (t Tokens) Add(o Tokens) Tokens {
	return Tokens{
		Input:     t.Input + o.Input,
		Cache5m:   t.Cache5m + o.Cache5m,
		Cache1h:   t.Cache1h + o.Cache1h,
		CacheRead: t.CacheRead + o.CacheRead,
		Output:    t.Output + o.Output,
	}
}

// Sub devolve a diferença campo a campo de t e o.
func (t Tokens) Sub(o Tokens) Tokens {
	return Tokens{
		Input:     t.Input - o.Input,
		Cache5m:   t.Cache5m - o.Cache5m,
		Cache1h:   t.Cache1h - o.Cache1h,
		CacheRead: t.CacheRead - o.CacheRead,
		Output:    t.Output - o.Output,
	}
}

// IsZero informa se todas as categorias estão zeradas.
func (t Tokens) IsZero() bool { return t == Tokens{} }

// ModelKey identifica um modelo numa velocidade. Fast é true quando
// usage.speed == "fast", cujo custo é 2× o padrão.
type ModelKey struct {
	Model string
	Fast  bool
}

// call é a contribuição de uma mensagem (um message.id) aos totais.
type call struct {
	key    ModelKey
	tokens Tokens
}

// File acompanha um transcript JSONL. O valor zero não é útil; use New.
// Os métodos podem ser chamados de várias goroutines.
type File struct {
	path string

	mu          sync.Mutex
	offset      int64
	info        os.FileInfo // arquivo visto no último Refresh, para detectar rotação
	calls       map[string]call
	totals      Tokens
	byModel     map[ModelKey]Tokens
	first, last time.Time
	parseErrors int
	anon        int // contador para mensagens sem message.id
}

// New cria um leitor para path sem tocar no disco; a leitura acontece em Refresh.
func New(path string) *File {
	f := &File{path: path}
	f.resetLocked()
	return f
}

// Path devolve o caminho do transcript.
func (f *File) Path() string { return f.path }

func (f *File) resetLocked() {
	f.offset = 0
	f.info = nil
	f.calls = make(map[string]call)
	f.totals = Tokens{}
	f.byModel = make(map[ModelKey]Tokens)
	f.first, f.last = time.Time{}, time.Time{}
	f.parseErrors = 0
	f.anon = 0
}

// Refresh lê o arquivo do último offset até a última linha completa. Uma linha
// final sem '\n' não é consumida: o offset fica antes dela e ela é relida
// quando for completada. Se o arquivo encolheu (truncamento) ou foi trocado por
// outro (rotação), a leitura recomeça do zero com os totais zerados.
//
// Arquivo inexistente devolve um erro em que errors.Is(err, fs.ErrNotExist) é
// verdadeiro; os totais já acumulados são mantidos.
func (f *File) Refresh() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	fh, err := os.Open(f.path)
	if err != nil {
		return fmt.Errorf("transcript: abrir %s: %w", f.path, err)
	}
	defer fh.Close()

	info, err := fh.Stat()
	if err != nil {
		return fmt.Errorf("transcript: stat %s: %w", f.path, err)
	}
	if info.Size() < f.offset || (f.info != nil && !os.SameFile(f.info, info)) {
		f.resetLocked()
	}
	f.info = info
	if info.Size() == f.offset {
		return nil
	}

	if _, err := fh.Seek(f.offset, io.SeekStart); err != nil {
		return fmt.Errorf("transcript: seek %s: %w", f.path, err)
	}
	r := bufio.NewReaderSize(fh, 64*1024)
	for {
		line, err := r.ReadBytes('\n')
		if err == io.EOF {
			// Linha incompleta (ou nada): não avança o offset.
			return nil
		}
		if err != nil {
			return fmt.Errorf("transcript: ler %s: %w", f.path, err)
		}
		f.offset += int64(len(line))
		f.processLocked(line)
	}
}

// line contém só os campos do transcript que interessam ao leitor.
type line struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	Message   *struct {
		ID    string `json:"id"`
		Model string `json:"model"`
		Usage *struct {
			InputTokens              int64  `json:"input_tokens"`
			CacheCreationInputTokens int64  `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     int64  `json:"cache_read_input_tokens"`
			OutputTokens             int64  `json:"output_tokens"`
			Speed                    string `json:"speed"`
			CacheCreation            *struct {
				Ephemeral5m int64 `json:"ephemeral_5m_input_tokens"`
				Ephemeral1h int64 `json:"ephemeral_1h_input_tokens"`
			} `json:"cache_creation"`
		} `json:"usage"`
	} `json:"message"`
}

func (f *File) processLocked(raw []byte) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return
	}
	var l line
	if err := json.Unmarshal(raw, &l); err != nil {
		f.parseErrors++
		return
	}
	if l.Timestamp != "" {
		if ts, err := time.Parse(time.RFC3339Nano, l.Timestamp); err == nil {
			if f.first.IsZero() || ts.Before(f.first) {
				f.first = ts
			}
			if ts.After(f.last) {
				f.last = ts
			}
		}
	}
	if l.Type != "assistant" || l.Message == nil || l.Message.Usage == nil {
		return
	}

	u := l.Message.Usage
	tok := Tokens{
		Input:     u.InputTokens,
		CacheRead: u.CacheReadInputTokens,
		Output:    u.OutputTokens,
	}
	if u.CacheCreation != nil {
		tok.Cache5m = u.CacheCreation.Ephemeral5m
		tok.Cache1h = u.CacheCreation.Ephemeral1h
	} else {
		tok.Cache5m = u.CacheCreationInputTokens
	}
	c := call{key: ModelKey{Model: l.Message.Model, Fast: u.Speed == "fast"}, tokens: tok}

	id := l.Message.ID
	if id == "" {
		// Sem id não há como deduplicar: cada linha conta por si.
		f.anon++
		id = "\x00anon-" + strconv.Itoa(f.anon)
	}
	if old, ok := f.calls[id]; ok {
		f.totals = f.totals.Sub(old.tokens)
		rest := f.byModel[old.key].Sub(old.tokens)
		if rest.IsZero() {
			delete(f.byModel, old.key)
		} else {
			f.byModel[old.key] = rest
		}
	}
	f.calls[id] = c
	f.totals = f.totals.Add(c.tokens)
	f.byModel[c.key] = f.byModel[c.key].Add(c.tokens)
}

// Totals devolve a soma deduplicada de todas as mensagens lidas.
func (f *File) Totals() Tokens {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.totals
}

// ByModel devolve uma cópia dos totais por modelo e velocidade.
func (f *File) ByModel() map[ModelKey]Tokens {
	f.mu.Lock()
	defer f.mu.Unlock()
	m := make(map[ModelKey]Tokens, len(f.byModel))
	for k, v := range f.byModel {
		m[k] = v
	}
	return m
}

// Messages devolve quantas mensagens distintas (por message.id) foram contadas.
func (f *File) Messages() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// FirstTS devolve o menor timestamp visto; zero se nenhum.
func (f *File) FirstTS() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.first
}

// LastTS devolve o maior timestamp visto; zero se nenhum.
func (f *File) LastTS() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.last
}

// ParseErrors devolve quantas linhas não eram JSON válido.
func (f *File) ParseErrors() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.parseErrors
}

// Offset devolve a posição, em bytes, logo após a última linha completa lida.
func (f *File) Offset() int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.offset
}
