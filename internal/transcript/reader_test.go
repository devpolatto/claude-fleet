package transcript

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// assistant monta uma linha sintética de transcript com usage simples.
func assistant(id, model string, in, out int64) string {
	return `{"type":"assistant","timestamp":"2026-01-02T10:00:00Z","message":{"id":"` + id +
		`","model":"` + model + `","usage":{"input_tokens":` + itoa(in) + `,"output_tokens":` + itoa(out) + `}}}`
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func refresh(t *testing.T, f *File) {
	t.Helper()
	if err := f.Refresh(); err != nil {
		t.Fatalf("Refresh: erro inesperado: %v", err)
	}
}

func appendFile(t *testing.T, path, s string) {
	t.Helper()
	fh, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fh.WriteString(s); err != nil {
		t.Fatal(err)
	}
	if err := fh.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestDedupPorMessageID(t *testing.T) {
	f := New(filepath.Join("testdata", "dedup.jsonl"))
	refresh(t, f)

	want := Tokens{Input: 18, Cache5m: 140, Cache1h: 200, CacheRead: 1002, Output: 62}
	if got := f.Totals(); got != want {
		t.Errorf("Totals = %+v, queria %+v", got, want)
	}
	if got := f.Messages(); got != 3 {
		t.Errorf("Messages = %d, queria 3", got)
	}
	if got := f.ParseErrors(); got != 0 {
		t.Errorf("ParseErrors = %d, queria 0", got)
	}

	wantModel := map[ModelKey]Tokens{
		{Model: "claude-opus-5-5"}:             {Input: 10, Cache5m: 100, Cache1h: 200, CacheRead: 1000, Output: 50},
		{Model: "claude-haiku-4-5"}:            {Input: 1, Cache5m: 40, CacheRead: 2, Output: 3},
		{Model: "claude-opus-5-5", Fast: true}: {Input: 7, Output: 9},
	}
	got := f.ByModel()
	if len(got) != len(wantModel) {
		t.Fatalf("ByModel tem %d chaves, queria %d: %+v", len(got), len(wantModel), got)
	}
	for k, w := range wantModel {
		if got[k] != w {
			t.Errorf("ByModel[%+v] = %+v, queria %+v", k, got[k], w)
		}
	}

	first := time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)
	if !f.FirstTS().Equal(first) {
		t.Errorf("FirstTS = %v, queria %v", f.FirstTS(), first)
	}
	if last := first.Add(5 * time.Second); !f.LastTS().Equal(last) {
		t.Errorf("LastTS = %v, queria %v", f.LastTS(), last)
	}
}

func TestDedupEntreRefreshes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	appendFile(t, path, assistant("msg_a", "m", 10, 1)+"\n")
	f := New(path)
	refresh(t, f)

	// O mesmo id chega depois com output maior: substitui, não soma.
	appendFile(t, path, assistant("msg_a", "m", 10, 30)+"\n")
	refresh(t, f)
	if got, want := f.Totals(), (Tokens{Input: 10, Output: 30}); got != want {
		t.Errorf("Totals = %+v, queria %+v", got, want)
	}

	// Trocar de modelo move a contribuição e remove a chave antiga.
	appendFile(t, path, assistant("msg_a", "n", 10, 30)+"\n")
	refresh(t, f)
	bm := f.ByModel()
	if _, ok := bm[ModelKey{Model: "m"}]; ok || len(bm) != 1 {
		t.Errorf("ByModel = %+v, queria só o modelo n", bm)
	}
}

func TestLinhaInvalidaPuladaEContada(t *testing.T) {
	f := New(filepath.Join("testdata", "invalid.jsonl"))
	refresh(t, f)
	if got := f.ParseErrors(); got != 2 {
		t.Errorf("ParseErrors = %d, queria 2", got)
	}
	if got, want := f.Totals(), (Tokens{Input: 11, Output: 22}); got != want {
		t.Errorf("Totals = %+v, queria %+v", got, want)
	}
}

func TestLinhaFinalIncompleta(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	full := assistant("msg_a", "m", 5, 6) + "\n"
	partial := assistant("msg_b", "m", 100, 200)
	appendFile(t, path, full+partial[:len(partial)/2])

	f := New(path)
	refresh(t, f)
	if got := f.Offset(); got != int64(len(full)) {
		t.Fatalf("Offset = %d, queria %d", got, len(full))
	}
	if got, want := f.Totals(), (Tokens{Input: 5, Output: 6}); got != want {
		t.Errorf("Totals = %+v, queria %+v", got, want)
	}
	if got := f.ParseErrors(); got != 0 {
		t.Errorf("ParseErrors = %d, queria 0 (linha incompleta não é erro)", got)
	}

	// Completa a linha sem '\n': ainda não conta.
	appendFile(t, path, partial[len(partial)/2:])
	refresh(t, f)
	if got := f.Offset(); got != int64(len(full)) {
		t.Fatalf("Offset = %d, queria %d", got, len(full))
	}

	appendFile(t, path, "\n")
	refresh(t, f)
	if got, want := f.Offset(), int64(len(full)+len(partial)+1); got != want {
		t.Errorf("Offset = %d, queria %d", got, want)
	}
	if got, want := f.Totals(), (Tokens{Input: 105, Output: 206}); got != want {
		t.Errorf("Totals = %+v, queria %+v", got, want)
	}
}

func TestRefreshSemMudancaMantemOffset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	appendFile(t, path, assistant("msg_a", "m", 1, 2)+"\n"+assistant("msg_b", "m", 3, 4)+"\n")
	f := New(path)
	refresh(t, f)
	off, tot := f.Offset(), f.Totals()

	refresh(t, f)
	if f.Offset() != off {
		t.Errorf("Offset mudou de %d para %d", off, f.Offset())
	}
	if f.Totals() != tot {
		t.Errorf("Totals mudou de %+v para %+v", tot, f.Totals())
	}
}

func TestTruncamentoRecomeca(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	appendFile(t, path, assistant("msg_a", "m", 100, 200)+"\n"+assistant("msg_b", "m", 100, 200)+"\n{ruim\n")
	f := New(path)
	refresh(t, f)
	if f.ParseErrors() != 1 {
		t.Fatalf("ParseErrors = %d, queria 1", f.ParseErrors())
	}

	short := assistant("msg_z", "x", 1, 1) + "\n"
	if err := os.WriteFile(path, []byte(short), 0o600); err != nil {
		t.Fatal(err)
	}
	refresh(t, f)
	if got, want := f.Totals(), (Tokens{Input: 1, Output: 1}); got != want {
		t.Errorf("Totals = %+v, queria %+v", got, want)
	}
	if bm := f.ByModel(); len(bm) != 1 {
		t.Errorf("ByModel = %+v, queria só o modelo x", bm)
	}
	if f.ParseErrors() != 0 {
		t.Errorf("ParseErrors = %d, queria 0 após recomeçar", f.ParseErrors())
	}
	if got := f.Offset(); got != int64(len(short)) {
		t.Errorf("Offset = %d, queria %d", got, len(short))
	}
}

func TestRotacaoRecomeca(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	appendFile(t, path, assistant("msg_a", "m", 1, 1)+"\n")
	f := New(path)
	refresh(t, f)

	// Outro arquivo, maior, assume o mesmo nome.
	other := filepath.Join(dir, "novo.jsonl")
	appendFile(t, other, assistant("msg_b", "n", 2, 2)+"\n"+assistant("msg_c", "n", 3, 3)+"\n")
	if err := os.Rename(other, path); err != nil {
		t.Fatal(err)
	}
	refresh(t, f)
	if got, want := f.Totals(), (Tokens{Input: 5, Output: 5}); got != want {
		t.Errorf("Totals = %+v, queria %+v", got, want)
	}
}

func TestArquivoInexistente(t *testing.T) {
	f := New(filepath.Join(t.TempDir(), "nao-existe.jsonl"))
	err := f.Refresh()
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Refresh erro = %v, queria fs.ErrNotExist", err)
	}
	if !f.Totals().IsZero() || f.Offset() != 0 {
		t.Errorf("estado deveria ficar zerado: Totals=%+v Offset=%d", f.Totals(), f.Offset())
	}
}

func TestCacheCreationAusenteContaComo5m(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	appendFile(t, path, `{"type":"assistant","message":{"id":"x","model":"m","usage":{"cache_creation_input_tokens":70}}}`+"\n")
	f := New(path)
	refresh(t, f)
	if got, want := f.Totals(), (Tokens{Cache5m: 70}); got != want {
		t.Errorf("Totals = %+v, queria %+v", got, want)
	}
}
