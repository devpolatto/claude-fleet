package transcript

import (
	"fmt"
	"os"
	"syscall"
)

// OpenRegular abre path só se for um arquivo regular. O O_NONBLOCK evita que
// um FIFO (ou dispositivo) em ~/.claude/projects/ trave quem abre esperando
// um escritor; depois do open, o Stat do próprio descritor recusa o que não
// for arquivo regular. Em arquivo regular o O_NONBLOCK não muda a leitura.
func OpenRegular(path string) (*os.File, error) {
	fh, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	info, err := fh.Stat()
	if err != nil {
		fh.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() {
		fh.Close()
		return nil, fmt.Errorf("%s não é um arquivo regular", path)
	}
	return fh, nil
}
