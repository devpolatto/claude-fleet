package store

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Limites da leitura do início do transcript: só as primeiras linhas e só
// alguns KiB, para que um transcript gigante não atrase o boot.
const (
	bootMaxLines = 20
	bootMaxBytes = 256 << 10
)

// transcriptHead é a lista branca dos campos lidos de uma linha do
// transcript no boot. Todo o resto (inclusive o conteúdo das mensagens) é
// descartado pelo decodificador.
type transcriptHead struct {
	Cwd       string `json:"cwd"`
	Timestamp string `json:"timestamp"`
}

// discovered é uma sessão encontrada em disco, antes de entrar no Store.
type discovered struct {
	id, path, cwd string
	started, last time.Time
}

// Bootstrap varre <projectsDir>/*/*.jsonl modificados há no máximo maxAge e cria
// sessões em desconhecido para as que o Store ainda não conhece.
//
// O session_id é o nome do arquivo sem .jsonl. Só entram arquivos que estão
// diretamente em <projectsDir>/<dir>/; os transcripts de subagentes, em
// <dir>/<session_id>/subagents/, ficam de fora. Cwd e Project vêm do campo
// cwd das primeiras linhas (vazios se não houver). LastEventAt é o mtime do
// arquivo; StartedAt é o primeiro timestamp válido do transcript, ou o mtime
// se não houver nenhum nas linhas lidas. Account sai do caminho do arquivo,
// pelas contas de WithAccounts. Sessões já conhecidas não são
// tocadas, e erros de leitura (diretório inexistente, arquivo ilegível) só
// fazem o item ser pulado.
func (s *Store) Bootstrap(projectsDir string, now time.Time, maxAge time.Duration) (added int) {
	found := scanProjects(projectsDir, now, maxAge)
	if len(found) == 0 {
		return 0
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range found {
		if _, known := s.sessions[d.id]; known {
			continue
		}
		sess := &Session{
			ID:             d.id,
			Cwd:            d.cwd,
			TranscriptPath: d.path,
			Account:        s.accounts.Resolve(d.path),
			Status:         StatusDesconhecido,
			StartedAt:      d.started,
			LastEventAt:    d.last,
			Subagents:      make(map[string]*Agent),
		}
		if d.cwd != "" {
			sess.Project = filepath.Base(d.cwd)
		}
		s.sessions[d.id] = sess
		added++
	}
	if added > 0 {
		s.notify()
	}
	return added
}

// scanProjects lista os transcripts recentes sem travar o Store. Usa
// os.ReadDir em vez de filepath.Glob para não depender de o caminho estar
// livre de metacaracteres de glob.
func scanProjects(projectsDir string, now time.Time, maxAge time.Duration) []discovered {
	dirs, err := os.ReadDir(projectsDir)
	if err != nil {
		return nil
	}
	var out []discovered
	for _, dir := range dirs {
		if !dir.IsDir() {
			continue
		}
		dirPath := filepath.Join(projectsDir, dir.Name())
		files, err := os.ReadDir(dirPath)
		if err != nil {
			continue
		}
		for _, f := range files {
			name := f.Name()
			id := strings.TrimSuffix(name, ".jsonl")
			if f.IsDir() || id == name || id == "" {
				continue
			}
			info, err := f.Info()
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			mtime := info.ModTime()
			if now.Sub(mtime) > maxAge {
				continue
			}
			path := filepath.Join(dirPath, name)
			cwd, first, ok := readHead(path)
			if !ok {
				continue
			}
			d := discovered{id: id, path: path, cwd: cwd, started: mtime, last: mtime}
			if !first.IsZero() {
				d.started = first
			}
			out = append(out, d)
		}
	}
	return out
}

// readHead lê as primeiras linhas do transcript e devolve o primeiro cwd e
// o primeiro timestamp encontrados. ok é false se o arquivo não pôde ser
// aberto; linhas que não são JSON válido são ignoradas.
func readHead(path string) (cwd string, first time.Time, ok bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", time.Time{}, false
	}
	defer f.Close()

	sc := bufio.NewScanner(io.LimitReader(f, bootMaxBytes))
	sc.Buffer(make([]byte, 0, 64<<10), bootMaxBytes)
	for n := 0; n < bootMaxLines && sc.Scan(); n++ {
		var h transcriptHead
		if json.Unmarshal(sc.Bytes(), &h) != nil {
			continue
		}
		if cwd == "" {
			cwd = h.Cwd
		}
		if first.IsZero() && h.Timestamp != "" {
			if t, err := time.Parse(time.RFC3339Nano, h.Timestamp); err == nil {
				first = t
			}
		}
		if cwd != "" && !first.IsZero() {
			break
		}
	}
	// Erro do scanner (linha maior que o limite, falha de leitura) só
	// encerra a leitura: o que já foi lido continua valendo.
	return cwd, first, true
}
