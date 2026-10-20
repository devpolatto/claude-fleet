package store

import "time"

// Limites da limpeza (STATE-03).
const (
	// endedTTL é quanto tempo uma sessão encerrada continua na grade,
	// contado a partir de EndedAt.
	endedTTL = 15 * time.Minute
	// idleTTL é quanto tempo uma sessão sem eventos continua na grade,
	// qualquer que seja o status, contado a partir do último evento.
	idleTTL = 12 * time.Hour
	// maxSessions limita o tamanho do mapa. Uma máquina real tem dezenas de
	// sessões nas últimas 12 h; passar disso só acontece com eventos forjados,
	// e aí sai a sessão parada há mais tempo.
	maxSessions = 500
)

// makeRoomLocked tira a sessão com a atividade mais antiga quando o mapa está
// cheio, antes de uma sessão nova entrar. Exige s.mu travado.
func (s *Store) makeRoomLocked() {
	if len(s.sessions) < maxSessions {
		return
	}
	var oldest string
	var at time.Time
	for id, sess := range s.sessions {
		if t := lastActivity(sess); oldest == "" || t.Before(at) {
			oldest, at = id, t
		}
	}
	delete(s.sessions, oldest)
}

// Prune remove sessões encerradas há mais de 15 min e sessões sem eventos há
// mais de 12 h.
//
// A regra dos 15 min vale só para sessão encerrada com EndedAt preenchido:
// uma sessão reaberta (UserPromptSubmit ou SessionStart depois do SessionEnd)
// tem EndedAt limpo e só sai pela regra das 12 h.
//
// A regra das 12 h usa o maior LastEventAt entre a sessão e seus subagentes.
// Um evento de subagente já atualiza o LastEventAt da sessão em Apply, então
// na prática os dois coincidem; o máximo só protege contra um estado montado
// por outro caminho (por exemplo, a descoberta no boot). Um subagente em
// ativo, mas sem eventos há mais de 12 h, não segura a sessão: o status
// sozinho não prova atividade, só os eventos provam.
//
// Os limites são estritos: com exatamente 15 min ou 12 h, a sessão fica.
// Prune avisa os assinantes (Subscribe) quando remove alguma sessão.
func (s *Store) Prune(now time.Time) (changed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for id, sess := range s.sessions {
		if expired(sess, now) {
			delete(s.sessions, id)
			changed = true
		}
	}
	if changed {
		s.notify()
	}
	return changed
}

// expired informa se a sessão deve sair da grade no instante now.
func expired(sess *Session, now time.Time) bool {
	if sess.Status == StatusEncerrado && sess.EndedAt != nil && now.Sub(*sess.EndedAt) > endedTTL {
		return true
	}
	return now.Sub(lastActivity(sess)) > idleTTL
}

// lastActivity devolve o maior LastEventAt entre a sessão e seus subagentes.
func lastActivity(sess *Session) time.Time {
	last := sess.LastEventAt
	for _, a := range sess.Subagents {
		if a.LastEventAt.After(last) {
			last = a.LastEventAt
		}
	}
	return last
}
