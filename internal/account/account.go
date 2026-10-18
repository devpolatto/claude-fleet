// Package account identifica de qual conta do Claude Code vem uma sessão.
//
// Com várias assinaturas na mesma máquina, cada conta tem o próprio
// diretório de configuração (CLAUDE_CONFIG_DIR), e os transcripts ficam em
// <dir>/projects/<projeto>/<session_id>.jsonl. A conta de uma sessão é a que
// contém o transcript_path (AD-009). Só o nome da conta sai deste pacote; o
// diretório fica no servidor.
package account

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Outra é o nome dado a um transcript fora de todas as contas declaradas.
const Outra = "outra"

// maxNome é o tamanho máximo do nome de uma conta (vai para a tela e para o
// arquivo de estado).
const maxNome = 20

// Account é uma conta do Claude Code: um nome e o diretório de configuração.
type Account struct {
	Name string // [a-z0-9-]{1,20}; vazio só na conta implícita
	Dir  string // diretório de configuração, absoluto e limpo
}

// ProjectsDir é o diretório dos transcripts da conta.
func (a Account) ProjectsDir() string { return filepath.Join(a.Dir, "projects") }

// List são as contas configuradas. É segura para uso concorrente; uma List
// nil resolve tudo para "".
type List struct {
	declared []Account // na ordem das flags, para log
	byLen    []Account // diretório mais longo primeiro, para Resolve

	mu        sync.Mutex
	seen      map[string]bool
	onUnknown func(configDir string)
}

// Parse lê as contas no formato nome=diretório. O diretório aceita ~ e ~/,
// expandidos com home, e precisa ser absoluto depois disso.
func Parse(specs []string, home string) (*List, error) {
	if len(specs) == 0 {
		return nil, errors.New("nenhuma conta informada")
	}
	var accs []Account
	nomes := make(map[string]bool)
	dirs := make(map[string]string)
	for _, s := range specs {
		name, dir, ok := strings.Cut(s, "=")
		if !ok {
			return nil, fmt.Errorf("conta %q: use nome=diretório", s)
		}
		if err := validName(name); err != nil {
			return nil, fmt.Errorf("conta %q: %w", s, err)
		}
		dir, err := expand(dir, home)
		if err != nil {
			return nil, fmt.Errorf("conta %q: %w", s, err)
		}
		if nomes[name] {
			return nil, fmt.Errorf("conta %q repetida", name)
		}
		if outro, ok := dirs[dir]; ok {
			return nil, fmt.Errorf("contas %q e %q usam o mesmo diretório %s", outro, name, dir)
		}
		nomes[name] = true
		dirs[dir] = name
		accs = append(accs, Account{Name: name, Dir: dir})
	}
	return newList(accs), nil
}

// Implicit é a conta única de quem não declarou nenhuma: Resolve devolve ""
// para ela, e a interface não mostra conta.
func Implicit(dir string) *List {
	return newList([]Account{{Dir: filepath.Clean(dir)}})
}

func newList(accs []Account) *List {
	byLen := append([]Account(nil), accs...)
	sort.SliceStable(byLen, func(i, j int) bool { return len(byLen[i].Dir) > len(byLen[j].Dir) })
	return &List{declared: accs, byLen: byLen, seen: make(map[string]bool)}
}

// All devolve as contas na ordem em que foram declaradas.
func (l *List) All() []Account {
	if l == nil {
		return nil
	}
	return append([]Account(nil), l.declared...)
}

// OnUnknown registra quem é avisado, uma vez por diretório de configuração,
// quando um transcript não pertence a nenhuma conta. O argumento é o
// diretório de configuração deduzido (vazio se o caminho não tiver a forma
// <dir>/projects/<projeto>/<arquivo>), nunca o caminho do transcript.
func (l *List) OnUnknown(f func(configDir string)) {
	l.mu.Lock()
	l.onUnknown = f
	l.mu.Unlock()
}

// Resolve devolve o nome da conta que contém o transcript: a de diretório
// mais longo, se uma conta estiver dentro de outra. Fora de todas, devolve
// Outra. O caminho é só limpo (filepath.Clean); links simbólicos não são
// resolvidos.
func (l *List) Resolve(transcriptPath string) string {
	if l == nil || transcriptPath == "" {
		return ""
	}
	p := filepath.Clean(transcriptPath)
	for _, a := range l.byLen {
		if within(p, a.ProjectsDir()) {
			return a.Name
		}
	}

	cfg := configDir(p)
	l.mu.Lock()
	f := l.onUnknown
	first := !l.seen[cfg]
	l.seen[cfg] = true
	l.mu.Unlock()
	if first && f != nil {
		f(cfg)
	}
	return Outra
}

// within informa se p é root ou está abaixo dele, por segmento de caminho.
func within(p, root string) bool {
	return p == root || strings.HasPrefix(p, root+string(filepath.Separator))
}

// configDir deduz <dir> de <dir>/projects/<projeto>/<arquivo>.
func configDir(p string) string {
	proj := filepath.Dir(filepath.Dir(p))
	if filepath.Base(proj) != "projects" {
		return ""
	}
	return filepath.Dir(proj)
}

func validName(name string) error {
	if name == "" || len(name) > maxNome {
		return fmt.Errorf("nome precisa ter de 1 a %d caracteres", maxNome)
	}
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return errors.New("nome aceita só a-z, 0-9 e -")
		}
	}
	if name == Outra {
		return fmt.Errorf("o nome %q é reservado", Outra)
	}
	return nil
}

func expand(dir, home string) (string, error) {
	if dir == "~" || strings.HasPrefix(dir, "~/") {
		if home == "" {
			return "", errors.New("HOME não definido para expandir ~")
		}
		dir = filepath.Join(home, strings.TrimPrefix(dir, "~"))
	}
	if !filepath.IsAbs(dir) {
		return "", fmt.Errorf("diretório %q precisa ser absoluto", dir)
	}
	return filepath.Clean(dir), nil
}
