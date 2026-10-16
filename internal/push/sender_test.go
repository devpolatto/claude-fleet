package push

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// aparelho simula o lado do navegador: chaves da inscrição e decifragem.
type aparelho struct {
	priv *ecdh.PrivateKey
	auth []byte
}

func novoAparelho(t *testing.T) *aparelho {
	t.Helper()
	priv, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, 16)
	rand.Read(auth)
	return &aparelho{priv: priv, auth: auth}
}

func (a *aparelho) inscricao(endpoint string) Subscription {
	var s Subscription
	s.Endpoint = endpoint
	s.Keys.P256dh = base64.RawURLEncoding.EncodeToString(a.priv.PublicKey().Bytes())
	s.Keys.Auth = base64.RawURLEncoding.EncodeToString(a.auth)
	return s
}

// decifrar faz o caminho inverso da RFC 8291, como o navegador.
func (a *aparelho) decifrar(t *testing.T, body []byte) []byte {
	t.Helper()
	salt, rs, idlen := body[:16], binary.BigEndian.Uint32(body[16:20]), int(body[20])
	if rs != recordSize || idlen != 65 {
		t.Fatalf("cabeçalho aes128gcm: rs=%d idlen=%d", rs, idlen)
	}
	asPublic := body[21 : 21+idlen]
	as, err := ecdh.P256().NewPublicKey(asPublic)
	if err != nil {
		t.Fatal(err)
	}
	shared, _ := a.priv.ECDH(as)
	ikm, _ := hkdf.Key(sha256.New, shared, a.auth, "WebPush: info\x00"+string(a.priv.PublicKey().Bytes())+string(asPublic), 32)
	prk, _ := hkdf.Extract(sha256.New, ikm, salt)
	cek, _ := hkdf.Expand(sha256.New, prk, "Content-Encoding: aes128gcm\x00", 16)
	nonce, _ := hkdf.Expand(sha256.New, prk, "Content-Encoding: nonce\x00", 12)
	block, _ := aes.NewCipher(cek)
	gcm, _ := cipher.NewGCM(block)
	plain, err := gcm.Open(nil, nonce, body[21+idlen:], nil)
	if err != nil {
		t.Fatalf("decifrar: %v", err)
	}
	if plain[len(plain)-1] != 0x02 {
		t.Fatalf("delimitador = %x, queria 02", plain[len(plain)-1])
	}
	return plain[:len(plain)-1]
}

func novoServico(t *testing.T) (*Service, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := NewService(dir)
	if err != nil {
		t.Fatal(err)
	}
	return s, dir
}

// verificarVAPID confere o cabeçalho Authorization: JWT ES256 válido para a
// chave anunciada, com aud = origem do endpoint.
func verificarVAPID(t *testing.T, authz, origem, chave string) {
	t.Helper()
	if !strings.HasPrefix(authz, "vapid t=") {
		t.Fatalf("Authorization = %q", authz)
	}
	partes := strings.SplitN(strings.TrimPrefix(authz, "vapid t="), ", k=", 2)
	if len(partes) != 2 || partes[1] != chave {
		t.Fatalf("k= não é a chave pública do serviço")
	}
	jwt := strings.Split(partes[0], ".")
	if len(jwt) != 3 {
		t.Fatalf("JWT com %d partes", len(jwt))
	}
	var claims struct {
		Aud string `json:"aud"`
		Exp int64  `json:"exp"`
		Sub string `json:"sub"`
	}
	cj, _ := base64.RawURLEncoding.DecodeString(jwt[1])
	if err := json.Unmarshal(cj, &claims); err != nil {
		t.Fatal(err)
	}
	if claims.Aud != origem || claims.Sub == "" || claims.Exp <= time.Now().Unix() || claims.Exp > time.Now().Add(24*time.Hour).Unix() {
		t.Errorf("claims = %+v", claims)
	}
	pub, _ := base64.RawURLEncoding.DecodeString(chave)
	x, y := elliptic.Unmarshal(elliptic.P256(), pub) //nolint:staticcheck // só para montar a chave do teste
	sig, _ := base64.RawURLEncoding.DecodeString(jwt[2])
	sum := sha256.Sum256([]byte(jwt[0] + "." + jwt[1]))
	if len(sig) != 64 || !ecdsa.Verify(&ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y}, sum[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
		t.Error("assinatura ES256 inválida")
	}
}

func TestSendAllEntregaCifradoEComVAPID(t *testing.T) {
	svc, _ := novoServico(t)
	ap := novoAparelho(t)
	var got []byte
	var hdr http.Header
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hdr = r.Header.Clone()
		got, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusCreated)
	}))
	defer ts.Close()
	svc.client = ts.Client()
	if err := svc.Subscribe(ap.inscricao(ts.URL + "/push/abc")); err != nil {
		t.Fatal(err)
	}

	msg := Message{Title: "Claude Fleet", Body: "toni-voz · aguardando permissão", Tag: "fleet-s1", URL: "/"}
	if n := svc.SendAll(context.Background(), msg, "topico"); n != 1 {
		t.Fatalf("SendAll = %d, queria 1", n)
	}
	for k, v := range map[string]string{"Content-Encoding": "aes128gcm", "Urgency": "high", "Topic": "topico", "TTL": "600"} {
		if hdr.Get(k) != v {
			t.Errorf("%s = %q, queria %q", k, hdr.Get(k), v)
		}
	}
	verificarVAPID(t, hdr.Get("Authorization"), ts.URL, svc.PublicKey())
	var back Message
	if err := json.Unmarshal(ap.decifrar(t, got), &back); err != nil || back != msg {
		t.Errorf("mensagem decifrada = %+v, %v", back, err)
	}
}

func TestSendAllApagaInscricaoVencida(t *testing.T) {
	svc, _ := novoServico(t)
	codigo := http.StatusGone
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(codigo) }))
	defer ts.Close()
	svc.client = ts.Client()
	svc.Subscribe(novoAparelho(t).inscricao(ts.URL + "/a"))
	svc.Subscribe(novoAparelho(t).inscricao(ts.URL + "/b"))

	codigo = http.StatusInternalServerError
	if n := svc.SendAll(context.Background(), Message{}, ""); n != 0 || svc.Count() != 2 {
		t.Errorf("5xx: enviados = %d, inscritos = %d; queria 0 e 2 (mantém)", n, svc.Count())
	}
	codigo = http.StatusGone
	svc.SendAll(context.Background(), Message{}, "")
	if svc.Count() != 0 {
		t.Errorf("410: inscritos = %d, queria 0", svc.Count())
	}
}

func TestInscricoesPersistemEChaveVAPIDEEstavel(t *testing.T) {
	svc, dir := novoServico(t)
	ap := novoAparelho(t)
	if err := svc.Subscribe(ap.inscricao("https://push.example/x")); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"vapid.pem", "push-subscriptions.json"} {
		fi, err := os.Stat(filepath.Join(dir, f))
		if err != nil || fi.Mode().Perm() != 0o600 {
			t.Errorf("%s: %v, perm %v; queria 600", f, err, fi.Mode().Perm())
		}
	}
	reaberto, err := NewService(dir)
	if err != nil {
		t.Fatal(err)
	}
	if reaberto.PublicKey() != svc.PublicKey() || reaberto.Count() != 1 {
		t.Errorf("depois de reabrir: chave igual = %v, inscritos = %d", reaberto.PublicKey() == svc.PublicKey(), reaberto.Count())
	}
	if err := reaberto.Unsubscribe("https://push.example/x"); err != nil || reaberto.Count() != 0 {
		t.Errorf("Unsubscribe: %v, inscritos = %d", err, reaberto.Count())
	}
}

func TestSubscriptionValidate(t *testing.T) {
	ap := novoAparelho(t)
	if err := ap.inscricao("https://fcm.googleapis.com/fcm/send/x").Validate(); err != nil {
		t.Errorf("inscrição boa: %v", err)
	}
	ruins := []Subscription{
		ap.inscricao("http://fcm.googleapis.com/x"), // sem TLS
		ap.inscricao("file:///etc/passwd"),
		ap.inscricao("https://"),
	}
	semAuth := ap.inscricao("https://push.example/x")
	semAuth.Keys.Auth = "curto"
	chaveRuim := ap.inscricao("https://push.example/x")
	chaveRuim.Keys.P256dh = base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{1}, 10))
	ruins = append(ruins, semAuth, chaveRuim)
	for i, s := range ruins {
		if s.Validate() == nil {
			t.Errorf("inscrição ruim %d aceita: %+v", i, s)
		}
	}
}
