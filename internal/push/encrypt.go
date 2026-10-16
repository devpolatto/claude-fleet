// Package push envia notificações Web Push aos aparelhos inscritos no
// painel: autenticação VAPID (RFC 8292), criptografia do conteúdo (RFC 8291,
// aes128gcm) e o conjunto de inscrições, tudo com a biblioteca padrão.
package push

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// recordSize é o "rs" do cabeçalho aes128gcm. A mensagem cabe num registro só.
const recordSize = 4096

// maxPlaintext deixa espaço para o delimitador (1 byte) e a tag GCM (16).
const maxPlaintext = recordSize - 17

// encrypt cifra plaintext para o aparelho dono de uaPublic (p256dh, ponto
// P-256 não comprimido) e authSecret (auth, 16 bytes), devolvendo o corpo
// aes128gcm: salt(16) || rs(4) || idlen(1) || chave pública efêmera(65) ||
// texto cifrado. rnd fornece a chave efêmera e o salt.
func encrypt(rnd io.Reader, uaPublic, authSecret, plaintext []byte) ([]byte, error) {
	asPriv, err := ecdh.P256().GenerateKey(rnd)
	if err != nil {
		return nil, fmt.Errorf("chave efêmera: %w", err)
	}
	salt := make([]byte, 16)
	if _, err := io.ReadFull(rnd, salt); err != nil {
		return nil, fmt.Errorf("salt: %w", err)
	}
	return encryptWith(asPriv, salt, uaPublic, authSecret, plaintext)
}

// encryptWith é encrypt com a chave efêmera e o salt fixos (usado no teste
// com o exemplo da RFC 8291).
func encryptWith(asPriv *ecdh.PrivateKey, salt, uaPublic, authSecret, plaintext []byte) ([]byte, error) {
	if len(plaintext) > maxPlaintext {
		return nil, errors.New("conteúdo grande demais para um registro")
	}
	if len(authSecret) != 16 {
		return nil, errors.New("auth deve ter 16 bytes")
	}
	ua, err := ecdh.P256().NewPublicKey(uaPublic)
	if err != nil {
		return nil, fmt.Errorf("p256dh inválida: %w", err)
	}
	shared, err := asPriv.ECDH(ua)
	if err != nil {
		return nil, fmt.Errorf("ecdh: %w", err)
	}
	asPublic := asPriv.PublicKey().Bytes()

	// IKM = HKDF(auth, ecdh_secret, "WebPush: info" || 0x00 || ua_public || as_public, 32)
	keyInfo := "WebPush: info\x00" + string(uaPublic) + string(asPublic)
	ikm, err := hkdf.Key(sha256.New, shared, authSecret, keyInfo, 32)
	if err != nil {
		return nil, err
	}
	prk, err := hkdf.Extract(sha256.New, ikm, salt)
	if err != nil {
		return nil, err
	}
	cek, err := hkdf.Expand(sha256.New, prk, "Content-Encoding: aes128gcm\x00", 16)
	if err != nil {
		return nil, err
	}
	nonce, err := hkdf.Expand(sha256.New, prk, "Content-Encoding: nonce\x00", 12)
	if err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	// Registro único (e último): conteúdo seguido do delimitador 0x02.
	rec := append(append(make([]byte, 0, len(plaintext)+1), plaintext...), 0x02)

	out := make([]byte, 0, 16+4+1+len(asPublic)+len(rec)+gcm.Overhead())
	out = append(out, salt...)
	out = binary.BigEndian.AppendUint32(out, recordSize)
	out = append(out, byte(len(asPublic)))
	out = append(out, asPublic...)
	return gcm.Seal(out, nonce, rec, nil), nil
}

// randReader permite trocar a fonte de aleatoriedade nos testes.
var randReader io.Reader = rand.Reader
