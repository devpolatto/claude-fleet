package hook

import (
	"os"
	"testing"
)

func TestScriptsEmbutidosIguaisAoArquivo(t *testing.T) {
	casos := []struct {
		nome    string
		arquivo string
		valor   string
	}{
		{"Script", "fleet-hook.sh", Script},
		{"StatusLine", "fleet-statusline.sh", StatusLine},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			if c.valor == "" {
				t.Fatalf("%s vazio", c.nome)
			}
			disco, err := os.ReadFile(c.arquivo)
			if err != nil {
				t.Fatalf("lendo %s: %v", c.arquivo, err)
			}
			if c.valor != string(disco) {
				t.Errorf("%s difere de %s em disco", c.nome, c.arquivo)
			}
		})
	}
}
