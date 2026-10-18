package account

import (
	"strings"
	"sync"
	"testing"
)

func TestParseRecusaNomeInvalido(t *testing.T) {
	tests := []struct {
		name, spec, wantErr string
	}{
		{"sem igual", "pessoal", "nome=diretório"},
		{"nome vazio", "=/c/p", "de 1 a 20"},
		{"nome longo demais", strings.Repeat("a", 21) + "=/c/p", "de 1 a 20"},
		{"maiúscula", "Pessoal=/c/p", "a-z"},
		{"espaço", "minha conta=/c/p", "a-z"},
		{"acento", "pessoál=/c/p", "a-z"},
		{"nome reservado", "outra=/c/p", "reservado"},
		{"diretório relativo", "pessoal=c/p", "absoluto"},
		{"diretório vazio", "pessoal=", "absoluto"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]string{tt.spec}, "/home/u")
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Parse(%q) erro = %v, queria conter %q", tt.spec, err, tt.wantErr)
			}
		})
	}
	if _, err := Parse(nil, "/home/u"); err == nil {
		t.Error("Parse(nil) deveria falhar")
	}
}

func TestParseExpandeTil(t *testing.T) {
	tests := []struct {
		name, spec, home, wantDir string
		wantErr                   bool
	}{
		{"til com barra", "p=~/.claude-pessoal", "/home/u", "/home/u/.claude-pessoal", false},
		{"til sozinho", "p=~", "/home/u", "/home/u", false},
		{"absoluto com sobra", "p=/home/u/./.claude//", "/home/u", "/home/u/.claude", false},
		{"til de outro usuário não expande", "p=~outro/.claude", "/home/u", "", true},
		{"til sem HOME", "p=~/.claude", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l, err := Parse([]string{tt.spec}, tt.home)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Parse(%q) erro = %v, queria erro %t", tt.spec, err, tt.wantErr)
			}
			if err == nil && l.All()[0].Dir != tt.wantDir {
				t.Errorf("Dir = %q, queria %q", l.All()[0].Dir, tt.wantDir)
			}
		})
	}
}

func TestParseRecusaRepetido(t *testing.T) {
	tests := []struct {
		name    string
		specs   []string
		wantErr string
	}{
		{"nome repetido", []string{"a=/c/a", "a=/c/b"}, "repetida"},
		{"mesmo diretório", []string{"a=/c/a", "b=/c/a/"}, "mesmo diretório"},
		{"mesmo diretório via til", []string{"a=~/.c", "b=/home/u/.c"}, "mesmo diretório"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(tt.specs, "/home/u")
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Parse(%q) erro = %v, queria conter %q", tt.specs, err, tt.wantErr)
			}
		})
	}
	l, err := Parse([]string{"b=/c/b", "a=/c/a"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if all := l.All(); all[0].Name != "b" || all[1].Name != "a" {
		t.Errorf("All() = %v, queria a ordem declarada", all)
	}
}

func TestResolvePrefixoMaisLongoVence(t *testing.T) {
	l, err := Parse([]string{
		"raiz=/home/u/.claude",
		"pessoal=/home/u/.claude-pessoal",
		"dentro=/home/u/.claude/projects/x/conta",
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, path, want string
	}{
		{"conta raiz", "/home/u/.claude/projects/-home-u-p/s1.jsonl", "raiz"},
		{"outra conta", "/home/u/.claude-pessoal/projects/-home-u-p/s1.jsonl", "pessoal"},
		{"conta dentro de outra vence", "/home/u/.claude/projects/x/conta/projects/p/s1.jsonl", "dentro"},
		{"prefixo de string sem fronteira", "/home/u/.claude/projectsX/p/s1.jsonl", Outra},
		{"fora do projects da conta", "/home/u/.claude/history.jsonl", Outra},
		{"vazio", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := l.Resolve(tt.path); got != tt.want {
				t.Errorf("Resolve(%q) = %q, queria %q", tt.path, got, tt.want)
			}
		})
	}

	impl := Implicit("/home/u/.claude")
	if got := impl.Resolve("/home/u/.claude/projects/p/s.jsonl"); got != "" {
		t.Errorf("conta implícita: Resolve = %q, queria vazio", got)
	}
	var nada *List
	if got := nada.Resolve("/qualquer/projects/p/s.jsonl"); got != "" {
		t.Errorf("List nil: Resolve = %q, queria vazio", got)
	}
}

func TestResolveForaDasContasViraOutraELogaUmaVez(t *testing.T) {
	l := Implicit("/home/u/.claude")
	var mu sync.Mutex
	avisos := map[string]int{}
	l.OnUnknown(func(dir string) {
		mu.Lock()
		avisos[dir]++
		mu.Unlock()
	})

	paths := []string{
		"/home/u/.claude-trabalho/projects/-a/s1.jsonl",
		"/home/u/.claude-trabalho/projects/-b/s2.jsonl",
		"/home/u/.claude-outra/projects/-a/s3.jsonl",
		"/tmp/solto.jsonl",
	}
	var wg sync.WaitGroup
	for range 20 {
		for _, p := range paths {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if got := l.Resolve(p); got != Outra {
					t.Errorf("Resolve(%q) = %q, queria %q", p, got, Outra)
				}
			}()
		}
	}
	wg.Wait()

	want := map[string]int{"/home/u/.claude-trabalho": 1, "/home/u/.claude-outra": 1, "": 1}
	if len(avisos) != len(want) {
		t.Fatalf("avisos = %v, queria %v", avisos, want)
	}
	for dir, n := range want {
		if avisos[dir] != n {
			t.Errorf("avisos[%q] = %d, queria %d", dir, avisos[dir], n)
		}
	}
	for dir := range avisos {
		if strings.HasSuffix(dir, ".jsonl") {
			t.Errorf("aviso com caminho de transcript: %q", dir)
		}
	}
}

func TestResolveLimpaPontoPonto(t *testing.T) {
	l, err := Parse([]string{"pessoal=/home/u/.claude-pessoal"}, "")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, path, want string
	}{
		{"ponto e barra dupla", "/home/u/.claude-pessoal/./projects//p/s.jsonl", "pessoal"},
		{"ponto-ponto que sai da conta", "/home/u/.claude-pessoal/projects/../../.claude-x/projects/p/s.jsonl", Outra},
		{"ponto-ponto que volta para a conta", "/home/u/.claude-x/../.claude-pessoal/projects/p/s.jsonl", "pessoal"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := l.Resolve(tt.path); got != tt.want {
				t.Errorf("Resolve(%q) = %q, queria %q", tt.path, got, tt.want)
			}
		})
	}
}
