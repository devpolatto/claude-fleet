package atomicfile

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// temporarios lista o que sobrou com o padrão de nome do temporário.
func temporarios(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var tmps []string
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			tmps = append(tmps, e.Name())
		}
	}
	return tmps
}

func TestWriteNaoDeixaTemporario(t *testing.T) {
	cases := []struct {
		name    string
		prepare func(t *testing.T, dir string) string // devolve o destino
		wantErr bool
	}{
		{
			name: "gravacao com sucesso",
			prepare: func(t *testing.T, dir string) string {
				return filepath.Join(dir, "estado.json")
			},
		},
		{
			name: "diretorio inexistente",
			prepare: func(t *testing.T, dir string) string {
				return filepath.Join(dir, "nao-existe", "estado.json")
			},
			wantErr: true,
		},
		{
			// O temporário chega a ser criado e escrito; quem falha é o
			// rename, por cima de um diretório com conteúdo.
			name: "rename falha",
			prepare: func(t *testing.T, dir string) string {
				path := filepath.Join(dir, "estado.json")
				if err := os.MkdirAll(filepath.Join(path, "dentro"), 0o700); err != nil {
					t.Fatal(err)
				}
				return path
			},
			wantErr: true,
		},
		{
			name: "diretorio sem permissao de escrita",
			prepare: func(t *testing.T, dir string) string {
				if os.Geteuid() == 0 {
					t.Skip("root ignora a permissão do diretório")
				}
				ro := filepath.Join(dir, "ro")
				if err := os.Mkdir(ro, 0o500); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { os.Chmod(ro, 0o700) })
				return filepath.Join(ro, "estado.json")
			},
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := tc.prepare(t, dir)
			err := Write(path, []byte(`{"ok":true}`), 0o600)
			if (err != nil) != tc.wantErr {
				t.Fatalf("Write() erro = %v, queria erro = %v", err, tc.wantErr)
			}
			// O temporário nasce no diretório do destino, quando ele existe.
			for _, d := range []string{dir, filepath.Dir(path)} {
				if _, statErr := os.Stat(d); statErr != nil {
					continue
				}
				if tmps := temporarios(t, d); len(tmps) > 0 {
					t.Errorf("sobrou temporário em %s: %v", d, tmps)
				}
			}
		})
	}
}

func TestWriteUsaPermissao(t *testing.T) {
	cases := []struct {
		name string
		perm fs.FileMode
	}{
		{"privado", 0o600},
		{"leitura para todos", 0o644},
		{"executavel", 0o755},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "arquivo")
			if err := Write(path, []byte("x"), tc.perm); err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if got := info.Mode().Perm(); got != tc.perm {
				t.Errorf("permissão = %v, queria %v", got, tc.perm)
			}
		})
	}
}

func TestWriteTrocaArquivoExistente(t *testing.T) {
	cases := []struct {
		name     string
		antes    []byte
		antesPrm fs.FileMode
		depois   []byte
		perm     fs.FileMode
	}{
		{"conteudo menor", []byte("conteúdo antigo e comprido"), 0o644, []byte("novo"), 0o600},
		{"conteudo maior", []byte("a"), 0o600, []byte("conteúdo novo bem maior"), 0o600},
		{"vazio", []byte("algo"), 0o600, []byte{}, 0o600},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "estado.json")
			if err := os.WriteFile(path, tc.antes, tc.antesPrm); err != nil {
				t.Fatal(err)
			}
			if err := Write(path, tc.depois, tc.perm); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, tc.depois) {
				t.Errorf("conteúdo = %q, queria %q", got, tc.depois)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if p := info.Mode().Perm(); p != tc.perm {
				t.Errorf("permissão = %v, queria %v", p, tc.perm)
			}
			if tmps := temporarios(t, dir); len(tmps) > 0 {
				t.Errorf("sobrou temporário: %v", tmps)
			}
		})
	}
}
