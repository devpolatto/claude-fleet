package push

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Message é o que o service worker transforma em notificação.
type Message struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Tag   string `json:"tag"` // mesma tag: a nova substitui a anterior
	URL   string `json:"url"` // aberta ao tocar
}

// messageTTL é quanto o serviço de push guarda a mensagem com o aparelho
// offline. Passado isso, o próximo lembrete (5 min) já terá saído.
const messageTTL = 10 * time.Minute

// Service junta chave VAPID, inscrições e envio.
type Service struct {
	vapid  *VAPID
	subs   *Subscriptions
	client *http.Client
	now    func() time.Time
}

// NewService prepara o push em dir (criado com permissão 700 se faltar):
// vapid.pem e push-subscriptions.json.
func NewService(dir string) (*Service, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	v, err := LoadOrCreateVAPID(filepath.Join(dir, "vapid.pem"))
	if err != nil {
		return nil, err
	}
	subs, err := LoadSubscriptions(filepath.Join(dir, "push-subscriptions.json"))
	if err != nil {
		return nil, err
	}
	return &Service{vapid: v, subs: subs, client: newClient(), now: time.Now}, nil
}

// newClient não segue redirect: a inscrição só é aceita com endpoint https, e
// um 307/308 do outro lado levaria o POST para qualquer destino, inclusive a
// rede local. O redirect volta como status e conta como falha de envio.
func newClient() *http.Client {
	return &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// PublicKey é a chave pública VAPID em base64url.
func (s *Service) PublicKey() string { return s.vapid.PublicKey() }

// Subscribe grava a inscrição de um aparelho.
func (s *Service) Subscribe(sub Subscription) error { return s.subs.Add(sub) }

// Unsubscribe apaga a inscrição do endpoint.
func (s *Service) Unsubscribe(endpoint string) error { return s.subs.Remove(endpoint) }

// Count é o número de aparelhos inscritos.
func (s *Service) Count() int { return len(s.subs.All()) }

// SendAll envia msg a todos os aparelhos e devolve quantos aceitaram. topic
// (até 32 caracteres base64url) deixa o serviço de push trocar uma mensagem
// ainda não entregue pela mais nova do mesmo assunto.
func (s *Service) SendAll(ctx context.Context, msg Message, topic string) int {
	payload, err := json.Marshal(msg)
	if err != nil {
		log.Printf("push: %v", err)
		return 0
	}
	sent := 0
	for _, sub := range s.subs.All() {
		gone, err := s.send(ctx, sub, payload, topic)
		switch {
		case gone:
			// 404/410: o aparelho saiu (desinscreveu ou desinstalou o app).
			if err := s.subs.Remove(sub.Endpoint); err != nil {
				log.Printf("push: apagar inscrição vencida: %v", err)
			}
		case err != nil:
			log.Printf("push: envio para %s falhou: %v", hostOf(sub.Endpoint), err)
		default:
			sent++
		}
	}
	return sent
}

// send faz o POST de uma mensagem; gone indica inscrição que não existe mais.
func (s *Service) send(ctx context.Context, sub Subscription, payload []byte, topic string) (gone bool, err error) {
	p256dh, _ := base64.RawURLEncoding.DecodeString(trimPad(sub.Keys.P256dh))
	auth, _ := base64.RawURLEncoding.DecodeString(trimPad(sub.Keys.Auth))
	body, err := encrypt(randReader, p256dh, auth, payload)
	if err != nil {
		return false, err
	}
	authz, err := s.vapid.authorization(sub.Endpoint, s.now())
	if err != nil {
		return false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sub.Endpoint, bytes.NewReader(body))
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", authz)
	req.Header.Set("Content-Encoding", "aes128gcm")
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("TTL", strconv.Itoa(int(messageTTL.Seconds())))
	req.Header.Set("Urgency", "high")
	if topic != "" {
		req.Header.Set("Topic", topic)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return false, nil
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return true, nil
	default:
		return false, fmt.Errorf("status %d", resp.StatusCode)
	}
}

// hostOf mostra só o host do endpoint no log: o caminho identifica o aparelho.
func hostOf(endpoint string) string {
	if u, err := http.NewRequest(http.MethodGet, endpoint, nil); err == nil {
		host := u.URL.Host
		host = strings.ReplaceAll(host, "\n", "")
		host = strings.ReplaceAll(host, "\r", "")
		return host
	}
	return "?"
}
