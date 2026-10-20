package web

import (
	"net"
	"net/netip"
	"sync"
	"time"
)

// Limites de tentativa de token errado por origem: passadas maxTokenFailures
// falhas dentro de failureWindow, a origem fica sem testar token até a janela
// acabar. O dono erra poucas vezes; quem tenta adivinhar fica com ~10
// palpites por minuto.
const (
	maxTokenFailures = 10
	failureWindow    = time.Minute
	// maxTrackedIPs limita a memória do mapa. Ao passar, as janelas vencidas
	// são descartadas (no máximo uma varredura por janela) e, se ainda faltar
	// espaço, sai uma entrada qualquer: memória e custo por falha ficam
	// limitados mesmo com origens novas a cada pedido.
	maxTrackedIPs = 1024
)

// failures conta tokens errados por origem (failureKey).
type failures struct {
	mu        sync.Mutex
	m         map[string]failureCount
	lastSweep time.Time
}

type failureCount struct {
	n     int
	start time.Time
}

// failureKey agrupa as falhas por origem: o IPv4 inteiro ou o /64 do IPv6,
// já que um único host costuma ter um /64 inteiro para variar o endereço.
func failureKey(remoteAddr string) string {
	host := remoteAddr
	if h, _, err := net.SplitHostPort(remoteAddr); err == nil {
		host = h
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return host
	}
	ip = ip.Unmap().WithZone("")
	if ip.Is6() {
		if p, err := ip.Prefix(64); err == nil {
			return p.String()
		}
	}
	return ip.String()
}

// blocked diz se a origem esgotou as tentativas da janela atual.
func (f *failures) blocked(key string, now time.Time) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.m[key]
	return ok && now.Sub(c.start) < failureWindow && c.n >= maxTokenFailures
}

// add registra um token errado vindo da origem key.
func (f *failures) add(key string, now time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.m == nil {
		f.m = make(map[string]failureCount)
	}
	c, known := f.m[key]
	if now.Sub(c.start) >= failureWindow {
		c = failureCount{start: now}
	}
	c.n++
	if !known && len(f.m) >= maxTrackedIPs {
		f.makeRoomLocked(now)
	}
	f.m[key] = c
}

// makeRoomLocked abre espaço para uma origem nova. Exige f.mu travado.
func (f *failures) makeRoomLocked(now time.Time) {
	if now.Sub(f.lastSweep) >= failureWindow {
		f.lastSweep = now
		for k, v := range f.m {
			if now.Sub(v.start) >= failureWindow {
				delete(f.m, k)
			}
		}
	}
	for k := range f.m {
		if len(f.m) < maxTrackedIPs {
			break
		}
		delete(f.m, k)
	}
}
