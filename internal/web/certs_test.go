package web

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// escreverPar grava um certificado autoassinado com o CN dado e força o
// mtime, para o teste não depender da resolução do relógio do disco.
func escreverPar(t *testing.T, dir, cn string, mod time.Time) (certFile, keyFile string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	kder, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certFile, keyFile = filepath.Join(dir, "tls.crt"), filepath.Join(dir, "tls.key")
	writeFileMod(t, certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), mod)
	writeFileMod(t, keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kder}), mod)
	return certFile, keyFile
}

func writeFileMod(t *testing.T, path string, data []byte, mod time.Time) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mod, mod); err != nil {
		t.Fatal(err)
	}
}

func cnDe(t *testing.T, c *tls.Certificate) string {
	t.Helper()
	x, err := x509.ParseCertificate(c.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	return x.Subject.CommonName
}

func TestCertReloaderRecarregaQuandoMuda(t *testing.T) {
	dir := t.TempDir()
	t0 := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	certFile, keyFile := escreverPar(t, dir, "antigo", t0)
	r, err := NewCertReloader(certFile, keyFile)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := r.GetCertificate(nil)
	if got := cnDe(t, c); got != "antigo" {
		t.Fatalf("CN = %q, queria antigo", got)
	}

	escreverPar(t, dir, "renovado", t0.Add(time.Hour))
	c, _ = r.GetCertificate(nil)
	if got := cnDe(t, c); got != "renovado" {
		t.Errorf("depois da renovação CN = %q, queria renovado", got)
	}
}

func TestCertReloaderMantemAnteriorSeArquivoQuebrado(t *testing.T) {
	dir := t.TempDir()
	t0 := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	certFile, keyFile := escreverPar(t, dir, "bom", t0)
	r, err := NewCertReloader(certFile, keyFile)
	if err != nil {
		t.Fatal(err)
	}
	// Renovação pela metade: o certificado mudou e ainda não é PEM válido.
	writeFileMod(t, certFile, []byte("meio escrito"), t0.Add(time.Hour))
	c, err := r.GetCertificate(nil)
	if err != nil || c == nil {
		t.Fatalf("GetCertificate = %v, %v; queria o par anterior", c, err)
	}
	if got := cnDe(t, c); got != "bom" {
		t.Errorf("CN = %q, queria bom", got)
	}
}

func TestNewCertReloaderErros(t *testing.T) {
	dir := t.TempDir()
	if _, err := NewCertReloader(filepath.Join(dir, "nao.crt"), filepath.Join(dir, "nao.key")); err == nil {
		t.Error("arquivos ausentes deveriam falhar")
	}
	bad := filepath.Join(dir, "bad.pem")
	writeFileMod(t, bad, []byte("lixo"), time.Now())
	if _, err := NewCertReloader(bad, bad); err == nil {
		t.Error("PEM inválido deveria falhar")
	}
}
