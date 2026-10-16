package web

import (
	"crypto/tls"
	"fmt"
	"log"
	"os"
	"sync"
	"time"
)

// CertReloader entrega o par certificado/chave lido do disco e o relê quando
// algum dos arquivos muda, para que a renovação (ex.: tailscale cert num
// timer) valha sem reiniciar o processo.
type CertReloader struct {
	certFile, keyFile string

	mu              sync.Mutex
	cert            *tls.Certificate
	certMod, keyMod time.Time
}

// NewCertReloader lê o par uma vez; erro aqui impede o servidor de subir.
func NewCertReloader(certFile, keyFile string) (*CertReloader, error) {
	r := &CertReloader{certFile: certFile, keyFile: keyFile}
	if err := r.reload(); err != nil {
		return nil, err
	}
	return r, nil
}

// GetCertificate serve de tls.Config.GetCertificate. A cada handshake compara
// o mtime dos arquivos; se mudou e a releitura falhar (ex.: renovação no meio
// da escrita), segue com o par anterior e tenta de novo no próximo handshake.
func (r *CertReloader) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cm, km, err := r.mtimes()
	if err == nil && (!cm.Equal(r.certMod) || !km.Equal(r.keyMod)) {
		if err := r.reloadLocked(); err != nil {
			log.Printf("web: certificado TLS não recarregado, mantendo o anterior: %v", err)
		} else {
			log.Printf("web: certificado TLS recarregado de %s", r.certFile)
		}
	}
	return r.cert, nil
}

func (r *CertReloader) reload() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.reloadLocked()
}

func (r *CertReloader) reloadLocked() error {
	cm, km, err := r.mtimes()
	if err != nil {
		return err
	}
	c, err := tls.LoadX509KeyPair(r.certFile, r.keyFile)
	if err != nil {
		return fmt.Errorf("carregar certificado TLS: %w", err)
	}
	r.cert, r.certMod, r.keyMod = &c, cm, km
	return nil
}

func (r *CertReloader) mtimes() (cert, key time.Time, err error) {
	ci, err := os.Stat(r.certFile)
	if err != nil {
		return cert, key, fmt.Errorf("certificado TLS: %w", err)
	}
	ki, err := os.Stat(r.keyFile)
	if err != nil {
		return cert, key, fmt.Errorf("chave TLS: %w", err)
	}
	return ci.ModTime(), ki.ModTime(), nil
}
