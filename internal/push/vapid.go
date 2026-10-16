package push

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

// vapidSubject é o contato exigido pelos serviços de push no claim "sub".
// Uma URL pública evita pôr um e-mail no token.
const vapidSubject = "https://github.com/devpolatto/claude-fleet"

// vapidTTL é a validade do JWT; o limite dos serviços é 24 h.
const vapidTTL = 12 * time.Hour

// VAPID guarda o par de chaves que identifica este servidor junto aos
// serviços de push (RFC 8292).
type VAPID struct {
	key *ecdsa.PrivateKey
	pub []byte // ponto P-256 não comprimido, 65 bytes
}

// LoadOrCreateVAPID lê o par de path (PEM "EC PRIVATE KEY") ou, se o arquivo
// não existir, gera um e o grava com permissão 600. Trocar a chave invalida
// as inscrições existentes, por isso ela é gerada uma vez só.
func LoadOrCreateVAPID(path string) (*VAPID, error) {
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		block, _ := pem.Decode(data)
		if block == nil || block.Type != "EC PRIVATE KEY" {
			return nil, fmt.Errorf("%s: PEM de chave EC esperado", path)
		}
		key, err := x509.ParseECPrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if key.Curve != elliptic.P256() {
			return nil, fmt.Errorf("%s: a chave VAPID deve ser P-256", path)
		}
		return newVAPID(key)
	case errors.Is(err, os.ErrNotExist):
		key, err := ecdsa.GenerateKey(elliptic.P256(), randReader)
		if err != nil {
			return nil, err
		}
		der, err := x509.MarshalECPrivateKey(key)
		if err != nil {
			return nil, err
		}
		if err := writeFileAtomic(path, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})); err != nil {
			return nil, err
		}
		return newVAPID(key)
	default:
		return nil, err
	}
}

func newVAPID(key *ecdsa.PrivateKey) (*VAPID, error) {
	ek, err := key.ECDH()
	if err != nil {
		return nil, err
	}
	return &VAPID{key: key, pub: ek.PublicKey().Bytes()}, nil
}

// PublicKey é a chave pública em base64url, o applicationServerKey que a
// página passa ao pushManager.subscribe.
func (v *VAPID) PublicKey() string {
	return base64.RawURLEncoding.EncodeToString(v.pub)
}

// authorization monta o cabeçalho "vapid t=<jwt>, k=<chave>" para o endpoint.
func (v *VAPID) authorization(endpoint string, now time.Time) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"typ":"JWT","alg":"ES256"}`))
	claims, err := json.Marshal(map[string]any{
		"aud": u.Scheme + "://" + u.Host,
		"exp": now.Add(vapidTTL).Unix(),
		"sub": vapidSubject,
	})
	if err != nil {
		return "", err
	}
	signing := header + "." + base64.RawURLEncoding.EncodeToString(claims)
	sum := sha256.Sum256([]byte(signing))
	r, s, err := ecdsa.Sign(randReader, v.key, sum[:])
	if err != nil {
		return "", err
	}
	// ES256 usa r || s com 32 bytes cada, não a codificação ASN.1.
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	jwt := signing + "." + base64.RawURLEncoding.EncodeToString(sig)
	return "vapid t=" + jwt + ", k=" + v.PublicKey(), nil
}

// writeFileAtomic grava data em path com permissão 600, via arquivo
// temporário e rename, para nunca deixar um arquivo pela metade.
func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op depois do rename
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
