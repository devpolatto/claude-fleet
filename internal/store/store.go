// Package store guarda em memória as sessões do Claude Code e seus
// subagentes, aplicando a máquina de estados do design aos eventos de hook.
//
// Resumo das transições da sessão (eventos sem agent_id):
//
//   - SessionStart cria a sessão em parado. Em sessão já conhecida não muda o
//     status (source=compact chega no meio da atividade; source=resume só
//     atualiza o transcript), exceto se ela estiver encerrada (reabre em
//     parado) ou em desconhecido, vinda do boot (vai para parado, salvo
//     source=compact, que não diz nada sobre o estado).
//   - UserPromptSubmit, PreToolUse, PostToolUse, PostToolUseFailure e
//     PostToolBatch são atividade: levam a ativo a partir de desconhecido,
//     parado ou aguardando. PostToolBatch e PostToolUseFailure também tiram
//     de aguardando, porque permissão negada não gera evento próprio.
//   - PermissionRequest leva a aguardando; Notification também, mas só a
//     partir de ativo (em parado ela é o aviso de ociosidade do modo
//     interativo, não um pedido de permissão).
//   - Stop (e StopFailure) leva a parado.
//   - SessionEnd leva a encerrado e preenche EndedAt.
//   - PreCompact, PostCompact e eventos não mapeados só atualizam LastEventAt.
//
// Depois do SessionEnd (encerrado), só UserPromptSubmit (reabre em ativo) e
// SessionStart (reabre em parado) mudam o status; os dois limpam EndedAt. Os
// demais eventos só atualizam LastEventAt. Isso cobre um --resume de sessão
// encerrada sem deixar que eventos atrasados (por exemplo, um PostToolUse
// assíncrono que chega depois do fim) ressuscitem a sessão.
//
// Eventos com agent_id alteram só o subagente, além do LastEventAt da
// sessão; ver applyAgent.
package store

import (
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/devpolatto/claude-fleet/internal/ingest"
)

// Nomes dos eventos de hook tratados pelo Store.
const (
	evSessionStart       = "SessionStart"
	evSessionEnd         = "SessionEnd"
	evUserPromptSubmit   = "UserPromptSubmit"
	evPreToolUse         = "PreToolUse"
	evPostToolUse        = "PostToolUse"
	evPostToolUseFailure = "PostToolUseFailure"
	evPostToolBatch      = "PostToolBatch"
	evPermissionRequest  = "PermissionRequest"
	evPermissionDenied   = "PermissionDenied"
	evNotification       = "Notification"
	evStop               = "Stop"
	evStopFailure        = "StopFailure"
	evSubagentStart      = "SubagentStart"
	evSubagentStop       = "SubagentStop"
)

// isActivity informa se o evento conta como atividade de ferramenta ou de
// prompt (leva a ativo e tira de aguardando).
func isActivity(name string) bool {
	switch name {
	case evUserPromptSubmit, evPreToolUse, evPostToolUse, evPostToolUseFailure, evPostToolBatch:
		return true
	}
	return false
}

// carriesTool informa se o evento traz um tool_name que vira LastTool.
func carriesTool(name string) bool {
	switch name {
	case evPreToolUse, evPostToolUse, evPostToolUseFailure:
		return true
	}
	return false
}

// Store é a máquina de estados das sessões. É seguro para uso concorrente.
type Store struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	subs     []chan struct{}
}

// New cria um Store vazio.
func New() *Store {
	return &Store{sessions: make(map[string]*Session)}
}

// Subscribe devolve um canal que recebe um sinal a cada mudança de estado.
// O canal tem buffer 1 e o envio não bloqueia: sinais seguidos enquanto o
// assinante não lê se fundem num só, e um assinante lento nunca trava Apply.
// Quem recebe o sinal deve ler o estado atual com Snapshot ou Get.
func (s *Store) Subscribe() <-chan struct{} {
	ch := make(chan struct{}, 1)
	s.mu.Lock()
	s.subs = append(s.subs, ch)
	s.mu.Unlock()
	return ch
}

// notify sinaliza todos os assinantes sem bloquear. Exige s.mu travado.
func (s *Store) notify() {
	for _, ch := range s.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// Apply aplica o evento ao estado e informa se algo mudou de fato. Eventos
// ignorados (por exemplo, SubagentStop de agente desconhecido) devolvem false.
func (s *Store) Apply(ev ingest.Event, now time.Time) (changed bool) {
	if ev.SessionID == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	sess, known := s.sessions[ev.SessionID]

	// SubagentStop de agente sem SubagentStart é ignorado por inteiro: a
	// compactação gera um desses e ele não deve criar mini-tile, sessão nem
	// mexer no LastEventAt.
	if ev.Name == evSubagentStop {
		if !known || ev.AgentID == "" || sess.Subagents[ev.AgentID] == nil {
			return false
		}
	}

	m := &mutation{}
	if !known {
		sess = &Session{
			ID:        ev.SessionID,
			Status:    StatusDesconhecido,
			StartedAt: now,
			Subagents: make(map[string]*Agent),
		}
		if ev.Name == evSessionStart {
			sess.Status = StatusParado
		}
		s.sessions[ev.SessionID] = sess
		m.changed = true
	}

	m.setTime(&sess.LastEventAt, now)
	// O cwd da sessão vem dos eventos da sessão principal. Um subagente pode
	// rodar em outro diretório (ex.: worktree) e não deve renomear o tile;
	// o cwd dele só vale enquanto a sessão ainda não tem nenhum.
	if ev.Cwd != "" && (ev.AgentID == "" || sess.Cwd == "") {
		m.setString(&sess.Cwd, ev.Cwd)
		m.setString(&sess.Project, filepath.Base(ev.Cwd))
	}
	if ev.TranscriptPath != "" {
		m.setString(&sess.TranscriptPath, ev.TranscriptPath)
	}

	if ev.AgentID != "" {
		applyAgent(m, sess, ev, now)
	} else if known {
		applySession(m, sess, ev, now)
	} else if ev.Name != evSessionStart {
		// Sessão recém-criada em desconhecido: o próprio evento decide o
		// primeiro estado (ex.: PreToolUse leva a ativo).
		applySession(m, sess, ev, now)
	}

	if m.changed {
		s.notify()
	}
	return m.changed
}

// applySession aplica um evento sem agent_id à sessão.
func applySession(m *mutation, sess *Session, ev ingest.Event, now time.Time) {
	if carriesTool(ev.Name) && ev.ToolName != "" && sess.Status != StatusEncerrado {
		// Inclui a ferramenta Agent: o Pre/Post dela só atualiza o
		// LastTool da sessão-pai, nunca o status de um subagente.
		m.setString(&sess.LastTool, ev.ToolName)
	}

	if sess.Status == StatusEncerrado {
		switch ev.Name {
		case evUserPromptSubmit:
			m.setStatus(&sess.Status, StatusAtivo)
			m.setTimePtr(&sess.EndedAt, nil)
		case evSessionStart:
			m.setStatus(&sess.Status, StatusParado)
			m.setTimePtr(&sess.EndedAt, nil)
		}
		return
	}

	switch {
	case isActivity(ev.Name):
		m.setStatus(&sess.Status, StatusAtivo)
	case ev.Name == evPermissionDenied:
		if sess.Status == StatusAguardando {
			m.setStatus(&sess.Status, StatusAtivo)
		}
	case ev.Name == evPermissionRequest:
		m.setStatus(&sess.Status, StatusAguardando)
	case ev.Name == evNotification:
		if sess.Status == StatusAtivo {
			m.setStatus(&sess.Status, StatusAguardando)
		}
	case ev.Name == evStop || ev.Name == evStopFailure:
		m.setStatus(&sess.Status, StatusParado)
	case ev.Name == evSessionEnd:
		m.setStatus(&sess.Status, StatusEncerrado)
		t := now
		m.setTimePtr(&sess.EndedAt, &t)
	case ev.Name == evSessionStart && sess.Status == StatusDesconhecido && ev.Source != "compact":
		// Sessão descoberta no boot (T16): o SessionStart é o primeiro
		// evento real e a leva a parado, como numa sessão nova. Com
		// source=compact a sessão pode estar em plena atividade, então
		// ela segue em desconhecido até o próximo evento de atividade.
		m.setStatus(&sess.Status, StatusParado)
	}
	// SessionStart em sessão conhecida fora de desconhecido, PreCompact,
	// PostCompact e eventos não mapeados: nada além do LastEventAt já
	// atualizado.
}

// applyAgent aplica um evento com agent_id. Só o subagente muda; da sessão,
// apenas LastEventAt (já atualizado por Apply), Cwd e TranscriptPath.
//
//   - SubagentStart cria o subagente em ativo (ou o reativa, se concluído).
//   - Atividade (PreToolUse, PostToolUse, PostToolUseFailure, PostToolBatch)
//     leva a ativo e atualiza LastTool; de agente desconhecido, cria o
//     subagente, porque o SubagentStart pode ter se perdido no envio.
//   - PermissionRequest e Notification levam a aguardando.
//   - SubagentStop leva a concluido, com EndedAt e o transcript do agente.
//
// Concluído é terminal: só um novo SubagentStart o reativa; outros eventos
// atrasados só atualizam o LastEventAt do subagente.
func applyAgent(m *mutation, sess *Session, ev ingest.Event, now time.Time) {
	ag := sess.Subagents[ev.AgentID]
	if ag == nil {
		var initial Status
		switch {
		case ev.Name == evSubagentStart || isActivity(ev.Name):
			initial = StatusAtivo
		case ev.Name == evPermissionRequest || ev.Name == evNotification:
			initial = StatusAguardando
		default:
			// SubagentStop já foi filtrado em Apply; demais eventos de um
			// agente desconhecido não bastam para criar mini-tile.
			return
		}
		ag = &Agent{ID: ev.AgentID, Type: ev.AgentType, Status: initial, StartedAt: now}
		sess.Subagents[ev.AgentID] = ag
		m.changed = true
	}

	m.setTime(&ag.LastEventAt, now)
	if ev.AgentType != "" {
		m.setString(&ag.Type, ev.AgentType)
	}

	if ev.Name == evSubagentStart {
		m.setStatus(&ag.Status, StatusAtivo)
		m.setTimePtr(&ag.EndedAt, nil)
		return
	}
	if ag.Status == StatusConcluido {
		return
	}

	switch {
	case isActivity(ev.Name):
		m.setStatus(&ag.Status, StatusAtivo)
		if carriesTool(ev.Name) && ev.ToolName != "" {
			m.setString(&ag.LastTool, ev.ToolName)
		}
	case ev.Name == evPermissionDenied:
		if ag.Status == StatusAguardando {
			m.setStatus(&ag.Status, StatusAtivo)
		}
	case ev.Name == evPermissionRequest || ev.Name == evNotification:
		m.setStatus(&ag.Status, StatusAguardando)
	case ev.Name == evSubagentStop:
		m.setStatus(&ag.Status, StatusConcluido)
		t := now
		m.setTimePtr(&ag.EndedAt, &t)
		if ev.AgentTranscriptPath != "" {
			m.setString(&ag.TranscriptPath, ev.AgentTranscriptPath)
		}
	}
}

// Snapshot devolve o resumo de todas as sessões, ordenado por StartedAt e
// depois por ID. O resultado é uma cópia e pode ser alterado à vontade.
func (s *Store) Snapshot() []SessionView {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]SessionView, 0, len(s.sessions))
	for _, sess := range s.sessions {
		v := SessionView{
			ID:          sess.ID,
			ShortID:     shortID(sess.ID),
			Project:     sess.Project,
			Name:        sess.Name,
			Cwd:         sess.Cwd,
			Status:      sess.Status,
			StartedAt:   sess.StartedAt,
			LastEventAt: sess.LastEventAt,
			EndedAt:     copyTime(sess.EndedAt),
			LastTool:    sess.LastTool,
			Total:       sess.total(),
			Subagents:   make([]AgentView, 0, len(sess.Subagents)),
		}
		for _, a := range sess.Subagents {
			v.Subagents = append(v.Subagents, AgentView{
				ID:          a.ID,
				Type:        a.Type,
				Status:      a.Status,
				StartedAt:   a.StartedAt,
				LastEventAt: a.LastEventAt,
				EndedAt:     copyTime(a.EndedAt),
				LastTool:    a.LastTool,
			})
		}
		sort.Slice(v.Subagents, func(i, j int) bool {
			return before(v.Subagents[i].StartedAt, v.Subagents[i].ID, v.Subagents[j].StartedAt, v.Subagents[j].ID)
		})
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool {
		return before(out[i].StartedAt, out[i].ID, out[j].StartedAt, out[j].ID)
	})
	return out
}

// Get devolve uma cópia profunda da sessão, com o Total calculado.
func (s *Store) Get(id string) (SessionDetail, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sess, ok := s.sessions[id]
	if !ok {
		return SessionDetail{}, false
	}
	return SessionDetail{Session: sess.clone(), Total: sess.total()}, true
}

// before ordena por instante e, no empate, por ID.
func before(ta time.Time, ida string, tb time.Time, idb string) bool {
	if !ta.Equal(tb) {
		return ta.Before(tb)
	}
	return ida < idb
}

// mutation acumula se alguma atribuição mudou um valor de fato.
type mutation struct{ changed bool }

func (m *mutation) setStatus(p *Status, v Status) {
	if *p != v {
		*p = v
		m.changed = true
	}
}

func (m *mutation) setString(p *string, v string) {
	if *p != v {
		*p = v
		m.changed = true
	}
}

func (m *mutation) setTime(p *time.Time, v time.Time) {
	if !p.Equal(v) {
		*p = v
		m.changed = true
	}
}

func (m *mutation) setTimePtr(p **time.Time, v *time.Time) {
	switch {
	case *p == nil && v == nil:
		return
	case *p != nil && v != nil && (*p).Equal(*v):
		return
	}
	*p = v
	m.changed = true
}
