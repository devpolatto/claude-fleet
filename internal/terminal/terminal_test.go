package terminal

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const (
	sessTmux = "11111111-1111-4111-8111-111111111111" // 4242.json, pane %11
	sessFora = "22222222-2222-4222-8222-222222222222" // 4343.json, sem tmux
	sessRuim = "33333333-3333-4333-8333-333333333333" // 4444.json, tmux malformado
)

// fakeRunner devolve out/err e registra as chamadas.
type fakeRunner struct {
	out   string
	err   error
	calls [][]string
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	return f.out, f.err
}

// fakeTTY codifica /dev/pts/N como o kernel no tty_nr (major 136).
func fakeTTY(path string) (uint64, error) {
	n, err := strconv.ParseUint(strings.TrimPrefix(path, "/dev/pts/"), 10, 8)
	if err != nil {
		return 0, err
	}
	return 136<<8 | n, nil
}

// procFake grava <proc>/<pid>/stat com o starttime e o tty_nr do tty.
func procFake(t *testing.T, root, pid, starttime, tty string) {
	t.Helper()
	dir := filepath.Join(root, pid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Campos 3 a 21 de enchimento, 22 = starttime; o nome tem espaço e ')'.
	fields := make([]string, 0, 30)
	fields = append(fields, "S")
	for i := 4; i <= 21; i++ {
		fields = append(fields, "0")
	}
	fields = append(fields, starttime, "0", "0")
	if tty != "" {
		dev, _ := fakeTTY(tty)
		fields[7-3] = strconv.FormatUint(dev, 10)
	}
	stat := pid + " (claude (x) y) " + strings.Join(fields, " ") + "\n"
	if err := os.WriteFile(filepath.Join(dir, "stat"), []byte(stat), 0o644); err != nil {
		t.Fatal(err)
	}
}

// setup copia testdata/sessions para <tmp>/conta/sessions e devolve o
// diretório da conta e a raiz do /proc falso.
func setup(t *testing.T) (configDir, proc string) {
	t.Helper()
	tmp := t.TempDir()
	configDir = filepath.Join(tmp, "conta")
	dst := filepath.Join(configDir, "sessions")
	if err := os.MkdirAll(dst, 0o700); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir("testdata/sessions")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join("testdata/sessions", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dst, e.Name()), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	proc = filepath.Join(tmp, "proc")
	return configDir, proc
}

func TestLocateAchaPaneDaSessao(t *testing.T) {
	cfg, proc := setup(t)
	procFake(t, proc, "4242", "2670278", "/dev/pts/13")
	run := &fakeRunner{out: "dev\t1\t0\t/dev/pts/13\n"}
	term := &Terminal{run: run, proc: proc, ttyDev: fakeTTY}

	got := term.Locate(context.Background(), cfg, sessTmux)
	want := Location{Estado: EstadoTmux, Rotulo: "tmux dev:1.0", Pane: "%11"}
	if got != want {
		t.Fatalf("Locate = %+v, quer %+v", got, want)
	}
	wantCall := []string{"tmux", "display-message", "-p", "-t", "%11", locateFormat}
	if len(run.calls) != 1 || !reflect.DeepEqual(run.calls[0], wantCall) {
		t.Errorf("chamadas = %q, quer só %q", run.calls, wantCall)
	}
}

func TestLocateEstados(t *testing.T) {
	tests := []struct {
		name      string
		session   string
		starttime string // do /proc falso; "" = processo morto
		tty       string
		out       string
		runErr    error
		want      Estado
		querTmux  bool // se o tmux deve ter sido chamado
	}{
		{"sem campo tmux é fora", sessFora, "1584847", "/dev/pts/1", "", nil, EstadoFora, false},
		{"procStart diferente dá desconhecido", sessTmux, "1", "/dev/pts/13", "dev\t1\t0\t/dev/pts/13\n", nil, EstadoDesconhecido, false},
		{"processo morto dá desconhecido", sessTmux, "", "", "", nil, EstadoDesconhecido, false},
		{"tty não bate dá desconhecido", sessTmux, "2670278", "/dev/pts/4", "dev\t1\t0\t/dev/pts/13\n", nil, EstadoDesconhecido, true},
		{"tmux fora do ar dá desconhecido", sessTmux, "2670278", "/dev/pts/13", "", errors.New("no server running"), EstadoDesconhecido, true},
		{"saída estranha do tmux dá desconhecido", sessTmux, "2670278", "/dev/pts/13", "dev\tx\t0\t/dev/pts/13\n", nil, EstadoDesconhecido, true},
		{"campo tmux malformado dá desconhecido", sessRuim, "999", "/dev/pts/13", "", nil, EstadoDesconhecido, false},
		{"sessão sem arquivo dá desconhecido", "44444444-4444-4444-8444-444444444444", "", "", "", nil, EstadoDesconhecido, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, proc := setup(t)
			if tt.starttime != "" {
				for _, pid := range []string{"4242", "4343", "4444"} {
					procFake(t, proc, pid, tt.starttime, tt.tty)
				}
			}
			run := &fakeRunner{out: tt.out, err: tt.runErr}
			got := (&Terminal{run: run, proc: proc, ttyDev: fakeTTY}).Locate(context.Background(), cfg, tt.session)
			if got.Estado != tt.want {
				t.Errorf("Estado = %q, quer %q", got.Estado, tt.want)
			}
			if got.Estado != EstadoTmux && (got.Rotulo != "" || got.Pane != "") {
				t.Errorf("fora do tmux, Location = %+v, quer rótulo e pane vazios", got)
			}
			if called := len(run.calls) > 0; called != tt.querTmux {
				t.Errorf("tmux chamado = %v, quer %v", called, tt.querTmux)
			}
		})
	}
}

func TestLocateArquivoGrandeIgnorado(t *testing.T) {
	cfg, proc := setup(t)
	procFake(t, proc, "4242", "2670278", "/dev/pts/13")
	path := filepath.Join(cfg, "sessions", "4242.json")
	data, _ := os.ReadFile(path)
	big := strings.Replace(string(data), `"status"`, `"pad":"`+strings.Repeat("x", maxSessionBytes)+`","status"`, 1)
	if err := os.WriteFile(path, []byte(big), 0o600); err != nil {
		t.Fatal(err)
	}
	run := &fakeRunner{out: "dev\t1\t0\t/dev/pts/13\n"}
	if got := (&Terminal{run: run, proc: proc, ttyDev: fakeTTY}).Locate(context.Background(), cfg, sessTmux); got.Estado != EstadoDesconhecido {
		t.Errorf("Estado = %q, quer desconhecido", got.Estado)
	}
}

func TestLocatePIDDoNomeDiferenteDoConteudoIgnorado(t *testing.T) {
	cfg, proc := setup(t)
	procFake(t, proc, "4242", "2670278", "/dev/pts/13")
	dir := filepath.Join(cfg, "sessions")
	if err := os.Rename(filepath.Join(dir, "4242.json"), filepath.Join(dir, "5000.json")); err != nil {
		t.Fatal(err)
	}
	run := &fakeRunner{out: "dev\t1\t0\t/dev/pts/13\n"}
	if got := (&Terminal{run: run, proc: proc, ttyDev: fakeTTY}).Locate(context.Background(), cfg, sessTmux); got.Estado != EstadoDesconhecido {
		t.Errorf("Estado = %q, quer desconhecido", got.Estado)
	}
}

func TestLocateRotuloSemCaractereDeControle(t *testing.T) {
	cfg, proc := setup(t)
	procFake(t, proc, "4242", "2670278", "/dev/pts/13")
	run := &fakeRunner{out: "a\x1b[31mb\x07\t1\t0\t/dev/pts/13\n"}
	got := (&Terminal{run: run, proc: proc, ttyDev: fakeTTY}).Locate(context.Background(), cfg, sessTmux)
	if got.Rotulo != "tmux a[31mb:1.0" {
		t.Errorf("Rotulo = %q", got.Rotulo)
	}
}

func TestOpenMontaComandoFixo(t *testing.T) {
	run := &fakeRunner{}
	err := (&Terminal{run: run}).Open(context.Background(), Location{Estado: EstadoTmux, Rotulo: "tmux dev:1.0", Pane: "%11"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"systemd-run", "--user", "--quiet", "--collect", "--expand-environment=no", "ptyxis", "--new-window",
		"-T", "claude-fleet · tmux dev:1.0", "--", "tmux", "attach-session", "-t", "%11"}
	if len(run.calls) != 1 || !reflect.DeepEqual(run.calls[0], want) {
		t.Fatalf("chamadas = %q, quer %q", run.calls, want)
	}
	if err := allowed(want[0], want[1:]); err != nil {
		t.Errorf("o comando montado por Open não passa na lista fechada: %v", err)
	}
}

func TestOpenRecusaForaDoTmux(t *testing.T) {
	for _, loc := range []Location{
		{Estado: EstadoFora},
		{Estado: EstadoDesconhecido},
		{Estado: EstadoTmux, Rotulo: "x", Pane: "%1;kill-server"},
	} {
		run := &fakeRunner{}
		if err := (&Terminal{run: run}).Open(context.Background(), loc); err == nil || len(run.calls) != 0 {
			t.Errorf("Open(%+v): err = %v, chamadas = %d; quer erro sem chamar nada", loc, err, len(run.calls))
		}
	}
}

// Nome de sessão longo, com caracteres de vários bytes ou com "$", ainda gera
// um comando que a lista fechada aceita (e o systemd não expande o "$").
func TestOpenTituloDeNomeLongoOuComCifrao(t *testing.T) {
	for _, nome := range []string{strings.Repeat("界", 200), strings.Repeat("🙂", 100), "x${HOME}$$"} {
		cfg, proc := setup(t)
		procFake(t, proc, "4242", "2670278", "/dev/pts/13")
		run := &fakeRunner{out: nome + "\t1\t0\t/dev/pts/13\n"}
		term := &Terminal{run: run, proc: proc, ttyDev: fakeTTY}
		loc := term.Locate(context.Background(), cfg, sessTmux)
		if loc.Estado != EstadoTmux {
			t.Fatalf("%q: Estado = %q", nome, loc.Estado)
		}
		if err := term.Open(context.Background(), loc); err != nil {
			t.Fatal(err)
		}
		call := run.calls[len(run.calls)-1]
		if err := allowed(call[0], call[1:]); err != nil {
			t.Errorf("%q: título recusado pela lista fechada: %v", nome, err)
		}
		if !slices.Contains(call, "--expand-environment=no") {
			t.Errorf("%q: falta --expand-environment=no", nome)
		}
	}
}

func TestStatRdevBateComTtyNr(t *testing.T) {
	data, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		t.Skip("sem /proc")
	}
	rest := strings.Fields(string(data[strings.LastIndexByte(string(data), ')')+1:]))
	link, err := os.Readlink("/proc/self/fd/0")
	if err != nil || rest[7-3] == "0" || !strings.HasPrefix(link, "/dev/pts/") {
		t.Skip("teste sem terminal")
	}
	dev, err := statRdev(link)
	if err != nil || strconv.FormatUint(dev, 10) != rest[7-3] {
		t.Errorf("statRdev(%s) = %d, %v; tty_nr = %s", link, dev, err, rest[7-3])
	}
	if _, err := statRdev("/etc/hostname"); err == nil {
		t.Error("statRdev aceitou arquivo comum")
	}
}

func TestOpenErroDoSystemdRun(t *testing.T) {
	run := &fakeRunner{err: errors.New("Failed to start transient service unit")}
	err := (&Terminal{run: run}).Open(context.Background(), Location{Estado: EstadoTmux, Rotulo: "tmux dev:1.0", Pane: "%11"})
	if err == nil || !strings.Contains(err.Error(), "transient") {
		t.Errorf("err = %v, quer o motivo do systemd-run", err)
	}
}

func TestRunnerRecusaComandoForaDaLista(t *testing.T) {
	open := openArgs("claude-fleet · tmux dev:1.0", "%11")
	mut := func(i int, v string) []string {
		a := append([]string(nil), open...)
		a[i] = v
		return a
	}
	tests := []struct {
		name string
		cmd  string
		args []string
	}{
		{"kill-server", "tmux", []string{"kill-server"}},
		{"send-keys", "tmux", []string{"send-keys", "-t", "%11", "rm -rf ~", "Enter"}},
		{"display-message com outro formato", "tmux", []string{"display-message", "-p", "-t", "%11", "#(id)"}},
		{"display-message com alvo inválido", "tmux", locateArgs("%11;x")},
		{"shell", "sh", []string{"-c", "id"}},
		{"systemd-run de outro programa", "systemd-run", mut(4, "xterm")},
		{"systemd-run com expansão de ambiente", "systemd-run", mut(3, "--expand-environment=yes")},
		{"systemd-run com outro comando tmux", "systemd-run", mut(11, "kill-server")},
		{"systemd-run com pane inválido", "systemd-run", mut(12, "=sessao")},
		{"título com controle", "systemd-run", mut(7, "a\nb")},
		{"systemd-run com argumento a mais", "systemd-run", append(append([]string(nil), open...), ";", "kill-server")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := (execRunner{}).Run(context.Background(), tt.cmd, tt.args...); !errors.Is(err, ErrNotAllowed) {
				t.Errorf("err = %v, quer ErrNotAllowed", err)
			}
		})
	}
	if err := allowed("tmux", locateArgs("%11")); err != nil {
		t.Errorf("display-message legítimo recusado: %v", err)
	}
}
