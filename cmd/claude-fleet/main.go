// Command claude-fleet serve o painel de sessões e subagentes do Claude Code.
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/devpolatto/claude-fleet/internal/store"
	"github.com/devpolatto/claude-fleet/internal/web"
)

const defaultAddr = "127.0.0.1:7777"

type config struct {
	Addr     string
	Token    string
	Projects string
	// TLSCert e TLSKey ligam HTTPS no endereço da tailnet; o loopback do
	// hook continua em HTTP.
	TLSCert, TLSKey string
}

const (
	bootstrapMaxAge = 30 * time.Minute
	pruneInterval   = time.Minute
	shutdownTimeout = 5 * time.Second
)

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "install-hooks" || os.Args[1] == "uninstall-hooks") {
		os.Exit(runHooksCmd(os.Args[1], os.Args[2:], os.Getenv, os.Stdin, os.Stdout, os.Stderr))
	}

	cfg, err := parseConfig(os.Args[1:], os.Getenv, os.Stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		log.Printf("claude-fleet: %v", err)
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, cfg); err != nil {
		log.Fatalf("claude-fleet: %v", err)
	}
}

// run liga Store, Refresher, Prune, Hub e servidor HTTP até ctx ser cancelado.
func run(ctx context.Context, cfg config) error {
	st := store.New()
	if n := st.Bootstrap(cfg.Projects, time.Now(), bootstrapMaxAge); n > 0 {
		log.Printf("claude-fleet: %d sessão(ões) recente(s) descoberta(s) no boot", n)
	}

	listeners, err := listen(cfg.Addr)
	if err != nil {
		return err
	}

	var tlsCfg *tls.Config
	if cfg.TLSCert != "" {
		certs, err := web.NewCertReloader(cfg.TLSCert, cfg.TLSKey)
		if err != nil {
			for _, ln := range listeners {
				ln.Close()
			}
			return err
		}
		tlsCfg = &tls.Config{GetCertificate: certs.GetCertificate, MinVersion: tls.VersionTLS12}
	}

	hub := web.NewHub(st)
	handler := web.NewServer(st, hub, cfg.Token).Handler()

	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); hub.Run(ctx) }()
	go func() { defer wg.Done(); store.NewRefresher(st).Run(ctx, store.DefaultRefreshInterval) }()
	go func() { defer wg.Done(); pruneLoop(ctx, st) }()

	serveErr := serve(ctx, handler, tlsCfg, listeners)
	wg.Wait()
	return serveErr
}

// serve atende cada listener com um http.Server próprio até ctx ser
// cancelado ou um deles falhar. listen devolve primeiro o endereço pedido;
// com tlsCfg, só ele vira HTTPS, e o loopback extra, usado pelo hook, segue
// em HTTP. Um servidor por listener porque um http.Server compartilhado
// entre Serve e ServeTLS pode ficar sem HTTP/2: se o Serve sobe primeiro, a
// conexão h2 negociada no TLS é fechada logo após o pedido.
func serve(ctx context.Context, handler http.Handler, tlsCfg *tls.Config, listeners []net.Listener) error {
	servers := make([]*http.Server, len(listeners))
	errc := make(chan error, len(listeners))
	for i, ln := range listeners {
		srv := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
		useTLS := i == 0 && tlsCfg != nil
		scheme := "http"
		if useTLS {
			srv.TLSConfig = tlsCfg
			scheme = "https"
		}
		servers[i] = srv
		log.Printf("claude-fleet: escutando em %s://%s", scheme, ln.Addr())
		go func(ln net.Listener) {
			var err error
			if useTLS {
				err = srv.ServeTLS(ln, "", "")
			} else {
				err = srv.Serve(ln)
			}
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				errc <- err
			}
		}(ln)
	}

	var serveErr error
	select {
	case <-ctx.Done():
	case serveErr = <-errc:
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	// O Hub encerra as conexões SSE quando ctx é cancelado; sem isso o
	// Shutdown esperaria por elas até o timeout.
	for _, srv := range servers {
		_ = srv.Shutdown(shutdownCtx)
	}
	return serveErr
}

// listen abre o endereço pedido e, se ele for um IP específico fora do
// loopback (ex.: o IP da tailnet), também 127.0.0.1 na mesma porta, para que
// o hook consiga entregar eventos em /event.
func listen(addr string) ([]net.Listener, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	lns := []net.Listener{ln}

	host, port, _ := net.SplitHostPort(addr)
	ip, err := netip.ParseAddr(host)
	if err != nil || ip.Unmap().IsLoopback() || ip.IsUnspecified() {
		return lns, nil
	}
	lo, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", port))
	if err != nil {
		ln.Close()
		return nil, fmt.Errorf("abrir loopback para o hook: %w", err)
	}
	return append(lns, lo), nil
}

func pruneLoop(ctx context.Context, st *store.Store) {
	t := time.NewTicker(pruneInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			st.Prune(now)
		}
	}
}

// parseConfig lê as flags e o ambiente e recusa um bind fora do loopback sem token.
func parseConfig(args []string, getenv func(string) string, out io.Writer) (config, error) {
	fs := flag.NewFlagSet("claude-fleet", flag.ContinueOnError)
	fs.SetOutput(out)

	cfg := config{}
	fs.StringVar(&cfg.Addr, "addr", defaultAddr, "endereço de escuta (host:porta)")
	fs.StringVar(&cfg.Token, "token", getenv("CLAUDE_FLEET_TOKEN"), "token de acesso; obrigatório fora do loopback (padrão: $CLAUDE_FLEET_TOKEN)")
	fs.StringVar(&cfg.Projects, "projects", "", "diretório dos transcripts (padrão: ~/.claude/projects)")
	fs.StringVar(&cfg.TLSCert, "tls-cert", "", "certificado TLS (PEM); liga HTTPS no endereço da tailnet, junto com -tls-key")
	fs.StringVar(&cfg.TLSKey, "tls-key", "", "chave do certificado TLS (PEM)")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	if fs.NArg() > 0 {
		return config{}, fmt.Errorf("argumento inesperado: %q", fs.Arg(0))
	}

	if cfg.Projects == "" {
		home := getenv("HOME")
		if home == "" {
			return config{}, errors.New("HOME não definido; informe -projects")
		}
		cfg.Projects = filepath.Join(home, ".claude", "projects")
	}

	if err := validateBind(cfg.Addr, cfg.Token); err != nil {
		return config{}, err
	}
	if err := validateTLS(cfg.Addr, cfg.TLSCert, cfg.TLSKey); err != nil {
		return config{}, err
	}
	return cfg, nil
}

// validateTLS exige -tls-cert e -tls-key juntos e só aceita TLS num IP
// específico fora do loopback: é o único caso em que listen abre também o
// 127.0.0.1 em HTTP, por onde o hook entrega os eventos.
func validateTLS(addr, cert, key string) error {
	if cert == "" && key == "" {
		return nil
	}
	if cert == "" || key == "" {
		return errors.New("-tls-cert e -tls-key vão juntos")
	}
	host, _, _ := net.SplitHostPort(addr)
	ip, err := netip.ParseAddr(host)
	if err != nil || ip.Unmap().IsLoopback() || ip.IsUnspecified() {
		return fmt.Errorf("TLS exige -addr num IP específico fora do loopback (ex.: o IP da tailnet), não %q: o hook precisa do 127.0.0.1 em HTTP", addr)
	}
	return nil
}

// validateBind exige token quando addr não é exclusivamente loopback.
// Host vazio (":7777") escuta em todas as interfaces e por isso também exige token.
func validateBind(addr, token string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("endereço inválido %q: %w", addr, err)
	}
	if port == "" {
		return fmt.Errorf("endereço inválido %q: porta ausente", addr)
	}
	if isLoopback(host) {
		return nil
	}
	if strings.TrimSpace(token) == "" {
		return fmt.Errorf("bind em %q expõe o painel fora do loopback; defina -token ou CLAUDE_FLEET_TOKEN", addr)
	}
	return nil
}

func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	return ip.Unmap().IsLoopback()
}
