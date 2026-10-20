package web

import (
	"net"
	"sync"
	"time"
)

// Limites de tentativa de token errado por IP: passadas maxTokenFailures
// falhas dentro de failureWindow, o IP fica sem testar token até a janela
// acabar. O dono erra poucas vezes; quem tenta adivinhar fica com ~10
// palpites por minuto.
const (
	maxTokenFailures = 10
	failureWindow    = time.Minute
	// maxTrackedIPs limita a memória do mapa; ao passar, as janelas vencidas
	// são descartadas.
	maxTrackedIPs = 1024
)

// failures conta tokens errados por IP de origem.
type failures struct {
	mu sync.Mutex
	m  map[string]failureCount
}

type failureCount struct {
	n     int
	start time.Time
}

// ipOf devolve o IP de remoteAddr, sem a porta.
func ipOf(remoteAddr string) string {
	if h, _, err := net.SplitHostPort(remoteAddr); err == nil {
		return h
	}
	return remoteAddr
}

// blocked diz se ip esgotou as tentativas da janela atual.
func (f *failures) blocked(ip string, now time.Time) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.m[ip]
	return ok && now.Sub(c.start) < failureWindow && c.n >= maxTokenFailures
}

// add registra um token errado vindo de ip.
func (f *failures) add(ip string, now time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.m == nil {
		f.m = make(map[string]failureCount)
	}
	c := f.m[ip]
	if now.Sub(c.start) >= failureWindow {
		c = failureCount{start: now}
	}
	c.n++
	f.m[ip] = c
	if len(f.m) > maxTrackedIPs {
		for k, v := range f.m {
			if now.Sub(v.start) >= failureWindow {
				delete(f.m, k)
			}
		}
	}
}
