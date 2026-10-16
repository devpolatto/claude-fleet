package push

import (
	"bytes"
	"crypto/ecdh"
	"encoding/base64"
	"testing"
)

func b64(t *testing.T, s string) []byte {
	t.Helper()
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		t.Fatalf("base64 %q: %v", s, err)
	}
	return b
}

// Exemplo do apêndice A da RFC 8291.
func TestEncryptExemploRFC8291(t *testing.T) {
	asPriv, err := ecdh.P256().NewPrivateKey(b64(t, "yfWPiYE-n46HLnH0KqZOF1fJJU3MYrct3AELtAQ-oRw"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := asPriv.PublicKey().Bytes(), b64(t, "BP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27mlmlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A8"); !bytes.Equal(got, want) {
		t.Fatalf("as_public não bate com o da RFC")
	}
	body, err := encryptWith(asPriv,
		b64(t, "DGv6ra1nlYgDCS1FRnbzlw"),
		b64(t, "BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4"),
		b64(t, "BTBZMqHH6r4Tts7J_aSIgg"),
		[]byte("When I grow up, I want to be a watermelon"))
	if err != nil {
		t.Fatal(err)
	}
	want := "DGv6ra1nlYgDCS1FRnbzlwAAEABBBP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27mlmlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A_yl95bQpu6cVPTpK4Mqgkf1CXztLVBSt2Ks3oZwbuwXPXLWyouBWLVWGNWQexSgSxsj_Qulcy4a-fN"
	if got := base64.RawURLEncoding.EncodeToString(body); got != want {
		t.Errorf("corpo cifrado não bate com a RFC 8291\n got: %s\nwant: %s", got, want)
	}
}
