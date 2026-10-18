// Package atomicfile grava arquivos de estado sem nunca deixar um arquivo
// pela metade: o conteúdo vai para um temporário no mesmo diretório, que
// depois troca de nome com o destino.
package atomicfile

import (
	"io/fs"
	"os"
	"path/filepath"
)

// Write grava data em path com a permissão perm, via arquivo temporário no
// mesmo diretório e rename. Se path já existe, é trocado inteiro; em caso de
// erro o temporário é apagado e o arquivo anterior fica como estava.
func Write(path string, data []byte, perm fs.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op depois do rename
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
