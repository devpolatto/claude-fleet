// Package ingest converte o payload JSON dos hooks do Claude Code num Event
// que carrega só metadados (decisão AD-003).
//
// A sanitização é feita por lista branca: o JSON é decodificado num struct
// privado que declara apenas os campos permitidos, e o encoding/json descarta
// todo o resto (prompt, tool_input, tool_response, message,
// last_assistant_message, error, compact_summary, custom_instructions,
// permission_suggestions, tool_calls etc.) sem que esse conteúdo chegue a
// existir fora do buffer de leitura. Nenhum erro devolvido por este pacote
// cita trechos do corpo.
package ingest

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Erros sentinela, detectáveis com errors.Is.
var (
	// ErrTooLarge indica corpo maior que o limite (o handler responde 413).
	ErrTooLarge = errors.New("ingest: corpo excede o tamanho máximo")
	// ErrInvalidJSON indica corpo que não é um objeto JSON válido.
	ErrInvalidJSON = errors.New("ingest: JSON inválido")
	// ErrMissingEvent indica hook_event_name ausente ou vazio.
	ErrMissingEvent = errors.New("ingest: hook_event_name ausente")
	// ErrMissingSession indica session_id ausente ou vazio. O Store indexa
	// tudo pela sessão, então um evento sem ela não tem onde ser aplicado.
	ErrMissingSession = errors.New("ingest: session_id ausente")
)

// Event é o evento de hook reduzido a metadados.
type Event struct {
	Name                string // hook_event_name
	Source              string // SessionStart: startup | resume | compact
	PermissionMode      string
	SessionID           string
	AgentID             string // vazio = sessão principal
	AgentType           string
	ToolName            string
	ToolUseID           string // PreToolUse, PostToolUse, PostToolUseFailure
	Reason              string // SessionEnd
	Trigger             string // PreCompact, PostCompact: manual | auto
	Cwd                 string
	TranscriptPath      string
	AgentTranscriptPath string // SubagentStop
}

// payload é a lista branca: só estes campos são lidos do JSON do hook.
// Acrescentar um campo aqui é decisão de segurança; nunca declare campos
// que carregam conteúdo de trabalho (ver tabela do spike T1 no design).
type payload struct {
	HookEventName       string `json:"hook_event_name"`
	Source              string `json:"source"`
	PermissionMode      string `json:"permission_mode"`
	SessionID           string `json:"session_id"`
	AgentID             string `json:"agent_id"`
	AgentType           string `json:"agent_type"`
	ToolName            string `json:"tool_name"`
	ToolUseID           string `json:"tool_use_id"`
	Reason              string `json:"reason"`
	Trigger             string `json:"trigger"`
	Cwd                 string `json:"cwd"`
	TranscriptPath      string `json:"transcript_path"`
	AgentTranscriptPath string `json:"agent_transcript_path"`
}

// Parse lê no máximo maxBytes de r e devolve o Event com os campos da lista
// branca. Corpo maior que maxBytes resulta em ErrTooLarge, sem ler além de
// maxBytes+1 bytes. Os erros de decodificação são reduzidos a ErrInvalidJSON
// sem a mensagem original do encoding/json, que pode citar valores do corpo.
func Parse(r io.Reader, maxBytes int64) (Event, error) {
	if maxBytes < 0 {
		maxBytes = 0
	}
	body, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return Event{}, fmt.Errorf("ingest: falha ao ler o corpo: %w", err)
	}
	if int64(len(body)) > maxBytes {
		return Event{}, ErrTooLarge
	}

	var p payload
	if err := json.Unmarshal(body, &p); err != nil {
		return Event{}, describeJSONError(err)
	}
	if p.HookEventName == "" {
		return Event{}, ErrMissingEvent
	}
	if p.SessionID == "" {
		return Event{}, ErrMissingSession
	}

	return Event{
		Name:                p.HookEventName,
		Source:              p.Source,
		PermissionMode:      p.PermissionMode,
		SessionID:           p.SessionID,
		AgentID:             p.AgentID,
		AgentType:           p.AgentType,
		ToolName:            p.ToolName,
		ToolUseID:           p.ToolUseID,
		Reason:              p.Reason,
		Trigger:             p.Trigger,
		Cwd:                 p.Cwd,
		TranscriptPath:      p.TranscriptPath,
		AgentTranscriptPath: p.AgentTranscriptPath,
	}, nil
}

// describeJSONError embrulha ErrInvalidJSON com informação estrutural
// (posição ou nome do campo), nunca com o valor recebido: o
// json.UnmarshalTypeError, por exemplo, pode citar o número lido.
func describeJSONError(err error) error {
	var syn *json.SyntaxError
	var typ *json.UnmarshalTypeError
	switch {
	case errors.As(err, &syn):
		return fmt.Errorf("%w: erro de sintaxe no byte %d", ErrInvalidJSON, syn.Offset)
	case errors.As(err, &typ):
		if typ.Field != "" {
			return fmt.Errorf("%w: tipo inesperado no campo %q", ErrInvalidJSON, typ.Field)
		}
		return fmt.Errorf("%w: o corpo não é um objeto", ErrInvalidJSON)
	default:
		return ErrInvalidJSON
	}
}
