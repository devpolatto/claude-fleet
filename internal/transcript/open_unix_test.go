//go:build unix

package transcript

import (
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// Um FIFO no lugar do transcript não trava o Refresh (que rodaria para
// sempre esperando um escritor) e devolve erro.
func TestRefreshRecusaFIFOSemTravar(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- New(path).Refresh() }()
	select {
	case err := <-done:
		if err == nil {
			t.Error("Refresh de FIFO deveria falhar")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Refresh travou no FIFO")
	}
	if _, ok := readSubagentMeta(path); ok {
		t.Error("readSubagentMeta aceitou um FIFO")
	}
}
