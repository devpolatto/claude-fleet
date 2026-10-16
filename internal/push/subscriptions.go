package push

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sort"
	"sync"
)

// Subscription é a inscrição de um aparelho, no formato de
// PushSubscription.toJSON() do navegador (sem expirationTime).
type Subscription struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

// Validate recusa inscrições malformadas. O endpoint precisa ser HTTPS: é
// para ele que o servidor faz POST.
func (s Subscription) Validate() error {
	u, err := url.Parse(s.Endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return errors.New("endpoint deve ser uma URL https")
	}
	if b, err := base64.RawURLEncoding.DecodeString(trimPad(s.Keys.P256dh)); err != nil || len(b) != 65 {
		return errors.New("p256dh inválida")
	}
	if b, err := base64.RawURLEncoding.DecodeString(trimPad(s.Keys.Auth)); err != nil || len(b) != 16 {
		return errors.New("auth inválido")
	}
	return nil
}

// trimPad aceita base64url com ou sem "=" no fim.
func trimPad(s string) string {
	for len(s) > 0 && s[len(s)-1] == '=' {
		s = s[:len(s)-1]
	}
	return s
}

// Subscriptions é o conjunto de inscrições, persistido em JSON (600).
type Subscriptions struct {
	path string

	mu   sync.Mutex
	subs map[string]Subscription // por endpoint
}

// LoadSubscriptions lê path; arquivo ausente é um conjunto vazio.
func LoadSubscriptions(path string) (*Subscriptions, error) {
	s := &Subscriptions{path: path, subs: make(map[string]Subscription)}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var list []Subscription
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	for _, sub := range list {
		if sub.Validate() == nil {
			s.subs[sub.Endpoint] = sub
		}
	}
	return s, nil
}

// Add grava (ou atualiza) a inscrição.
func (s *Subscriptions) Add(sub Subscription) error {
	if err := sub.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.subs[sub.Endpoint]; ok && old == sub {
		return nil
	}
	s.subs[sub.Endpoint] = sub
	return s.saveLocked()
}

// Remove apaga a inscrição do endpoint; apagar o que não existe não é erro.
func (s *Subscriptions) Remove(endpoint string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.subs[endpoint]; !ok {
		return nil
	}
	delete(s.subs, endpoint)
	return s.saveLocked()
}

// All devolve uma cópia das inscrições, ordenada por endpoint.
func (s *Subscriptions) All() []Subscription {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Subscription, 0, len(s.subs))
	for _, sub := range s.subs {
		out = append(out, sub)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Endpoint < out[j].Endpoint })
	return out
}

func (s *Subscriptions) saveLocked() error {
	list := make([]Subscription, 0, len(s.subs))
	for _, sub := range s.subs {
		list = append(list, sub)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Endpoint < list[j].Endpoint })
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(s.path, data)
}
