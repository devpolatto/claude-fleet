package store

import (
	"time"

	"github.com/devpolatto/claude-fleet/internal/pricing"
)

// Status é o estado de uma sessão ou de um subagente.
type Status string

// Estados possíveis. Sessões usam os cinco primeiros; subagentes usam
// ativo, aguardando e concluido.
const (
	StatusAtivo        Status = "ativo"
	StatusAguardando   Status = "aguardando"
	StatusParado       Status = "parado"
	StatusEncerrado    Status = "encerrado"
	StatusDesconhecido Status = "desconhecido"
	StatusConcluido    Status = "concluido"
)

// Usage é o consumo de tokens e o custo estimado. Fica zerado até o
// Refresher (T9) preenchê-lo com SetUsage.
type Usage struct {
	Tokens    pricing.Tokens
	CostUSD   float64
	CostKnown bool // false se algum modelo ficou sem preço
}

// add soma u e o; o custo só é conhecido se os dois forem.
func (u Usage) add(o Usage) Usage {
	return Usage{
		Tokens: pricing.Tokens{
			Input:     u.Tokens.Input + o.Tokens.Input,
			Cache5m:   u.Tokens.Cache5m + o.Tokens.Cache5m,
			Cache1h:   u.Tokens.Cache1h + o.Tokens.Cache1h,
			CacheRead: u.Tokens.CacheRead + o.Tokens.CacheRead,
			Output:    u.Tokens.Output + o.Tokens.Output,
		},
		CostUSD:   u.CostUSD + o.CostUSD,
		CostKnown: u.CostKnown && o.CostKnown,
	}
}

// Agent é um subagente lançado dentro de uma sessão.
type Agent struct {
	ID, Type, Description  string
	Status                 Status
	StartedAt, LastEventAt time.Time
	EndedAt                *time.Time // preenchido no SubagentStop
	LastTool               string
	TranscriptPath         string // agent_transcript_path do SubagentStop
	Usage                  Usage
	Models                 map[string]Usage
}

// Session é uma sessão do Claude Code.
type Session struct {
	ID, Cwd, Project, TranscriptPath string
	Status                           Status
	StartedAt, LastEventAt           time.Time
	EndedAt                          *time.Time // preenchido no SessionEnd
	LastTool                         string
	Main                             Usage // só a sessão principal
	Models                           map[string]Usage
	Subagents                        map[string]*Agent
}

// AgentView é o resumo de um subagente para o mini-tile.
type AgentView struct {
	ID, Type               string
	Status                 Status
	StartedAt, LastEventAt time.Time
	EndedAt                *time.Time
	LastTool               string
}

// SessionView é o resumo de uma sessão para o tile (enviado por SSE).
// Subagents vem ordenado por StartedAt e depois por ID.
type SessionView struct {
	ID, ShortID, Project, Cwd string
	Status                    Status
	StartedAt, LastEventAt    time.Time
	EndedAt                   *time.Time
	LastTool                  string
	Total                     Usage
	Subagents                 []AgentView
}

// SessionDetail é a sessão completa para o modal, com Total = Main + Σ
// Usage dos subagentes. É uma cópia profunda: alterá-la não afeta o Store.
type SessionDetail struct {
	Session
	Total Usage
}

// shortIDLen é o tamanho do id curto exibido no tile.
const shortIDLen = 8

func shortID(id string) string {
	if len(id) <= shortIDLen {
		return id
	}
	return id[:shortIDLen]
}

func copyTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	c := *t
	return &c
}

func copyModels(m map[string]Usage) map[string]Usage {
	if m == nil {
		return nil
	}
	c := make(map[string]Usage, len(m))
	for k, v := range m {
		c[k] = v
	}
	return c
}

// clone devolve uma cópia profunda do agente.
func (a *Agent) clone() *Agent {
	c := *a
	c.EndedAt = copyTime(a.EndedAt)
	c.Models = copyModels(a.Models)
	return &c
}

// clone devolve uma cópia profunda da sessão.
func (s *Session) clone() Session {
	c := *s
	c.EndedAt = copyTime(s.EndedAt)
	c.Models = copyModels(s.Models)
	c.Subagents = make(map[string]*Agent, len(s.Subagents))
	for id, a := range s.Subagents {
		c.Subagents[id] = a.clone()
	}
	return c
}

// total soma o uso da sessão principal com o de todos os subagentes.
func (s *Session) total() Usage {
	t := s.Main
	for _, a := range s.Subagents {
		t = t.add(a.Usage)
	}
	return t
}
