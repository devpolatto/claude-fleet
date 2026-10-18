// Package ratelimit lê o uso do plano (janelas de 5 h e semanal) do JSON que
// o Claude Code entrega ao comando de status line (decisão AD-007).
//
// Como o ingest, a leitura é por lista branca: o JSON é decodificado num
// struct privado que declara só rate_limits.five_hour, rate_limits.seven_day e
// transcript_path, e o encoding/json descarta todo o resto (cwd, session_id,
// model, workspace, cost, context_window, spend_limit etc.). O transcript_path
// só serve para descobrir a conta e nunca é gravado nem logado (AD-003).
// Nenhum erro devolvido por este pacote cita trechos do corpo.
package ratelimit

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
)

// MaxBytes é o tamanho máximo sugerido para o corpo de POST /statusline. O
// JSON do status line tem alguns KiB; 64 KiB deixa folga sem abrir espaço
// para abuso.
const MaxBytes = 64 << 10

// Erros sentinela, detectáveis com errors.Is.
var (
	// ErrTooLarge indica corpo maior que o limite (o handler responde 413).
	ErrTooLarge = errors.New("ratelimit: corpo excede o tamanho máximo")
	// ErrInvalidJSON indica corpo que não é um objeto JSON válido ou que traz
	// um dos campos aceitos com tipo inesperado.
	ErrInvalidJSON = errors.New("ratelimit: JSON inválido")
)

// Window é uma janela de limite do plano.
type Window struct {
	UsedPercentage float64 `json:"used_percentage"` // 0–100; pode ter casa decimal (ex.: 23.5)
	ResetsAt       int64   `json:"resets_at"`       // segundos Unix
}

// Reading é o que se aproveita de uma chamada do status line. Janela nil
// significa ausente (sem limite no plano, antes da 1ª resposta ou inválida).
type Reading struct {
	// TranscriptPath só vive até a conta ser resolvida; sem tag JSON de
	// propósito, para nunca ser serializado.
	TranscriptPath string
	FiveHour       *Window
	SevenDay       *Window
}

// payload é a lista branca: só estes campos são lidos do JSON do status
// line. Acrescentar um campo aqui é decisão de segurança (AD-003).
type payload struct {
	TranscriptPath string `json:"transcript_path"`
	RateLimits     *struct {
		FiveHour *rawWindow `json:"five_hour"`
		SevenDay *rawWindow `json:"seven_day"`
	} `json:"rate_limits"`
}

// rawWindow usa ponteiros para distinguir campo ausente ou null de zero.
// resets_at é lido como float64 para que um valor com casa decimal não
// derrube o corpo inteiro; a parte fracionária é descartada.
type rawWindow struct {
	UsedPercentage *float64 `json:"used_percentage"`
	ResetsAt       *float64 `json:"resets_at"`
}

// Parse lê no máximo maxBytes de r e devolve a Reading com os campos da lista
// branca. Corpo maior que maxBytes resulta em ErrTooLarge, sem ler além de
// maxBytes+1 bytes. Uma janela com used_percentage fora de [0, 100],
// resets_at <= 0 ou algum dos dois ausente vira nil, sem erro.
func Parse(r io.Reader, maxBytes int64) (Reading, error) {
	if maxBytes < 0 {
		maxBytes = 0
	}
	body, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return Reading{}, fmt.Errorf("ratelimit: falha ao ler o corpo: %w", err)
	}
	if int64(len(body)) > maxBytes {
		return Reading{}, ErrTooLarge
	}

	var p payload
	if err := json.Unmarshal(body, &p); err != nil {
		return Reading{}, describeJSONError(err)
	}

	out := Reading{TranscriptPath: p.TranscriptPath}
	if p.RateLimits != nil {
		out.FiveHour = p.RateLimits.FiveHour.valid()
		out.SevenDay = p.RateLimits.SevenDay.valid()
	}
	return out, nil
}

// valid devolve a janela se ela estiver completa e dentro dos limites, ou nil.
func (w *rawWindow) valid() *Window {
	if w == nil || w.UsedPercentage == nil || w.ResetsAt == nil {
		return nil
	}
	pct, reset := *w.UsedPercentage, math.Trunc(*w.ResetsAt)
	if pct < 0 || pct > 100 || reset <= 0 || reset > math.MaxInt64/2 {
		return nil
	}
	return &Window{UsedPercentage: pct, ResetsAt: int64(reset)}
}

// describeJSONError embrulha ErrInvalidJSON com informação estrutural
// (posição ou nome do campo), nunca com o valor recebido.
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
