package pricing

import (
	"math"
	"testing"
)

const milhao = 1_000_000

func TestCost(t *testing.T) {
	tests := []struct {
		name      string
		model     string
		tokens    Tokens
		fast      bool
		wantUSD   float64
		wantKnown bool
	}{
		{"opus 5.5: 1M input + 1M output", "claude-opus-5-5", Tokens{Input: milhao, Output: milhao}, false, 24.00, true},
		{"opus 5.5 em fast mode dobra", "claude-opus-5-5", Tokens{Input: milhao, Output: milhao}, true, 48.00, true},
		{"opus 5.5 não casa com a linha de opus 5", "claude-opus-5-5", Tokens{Input: milhao}, false, 4.00, true},
		{"opus 5 usa a própria linha", "claude-opus-5", Tokens{Input: milhao}, false, 5.00, true},
		{"opus 5.5 com sufixo [1m]", "claude-opus-5-5[1m]", Tokens{Output: milhao}, false, 20.00, true},
		{"opus 4.6 com sufixo de data", "claude-opus-4-6-20260101", Tokens{Input: milhao}, false, 5.00, true},
		{"fable 5.1 não casa com fable 5 (cache read)", "claude-fable-5-1", Tokens{CacheRead: milhao}, false, 0.25, true},
		{"fable 5 cache read", "claude-fable-5", Tokens{CacheRead: milhao}, false, 1.00, true},
		{"sonnet 5.5 cache write 5 min é 1.25× o input", "claude-sonnet-5-5", Tokens{Cache5m: milhao}, false, 2.50, true},
		{"sonnet 4.6 cache write 1 h é 2× o input", "claude-sonnet-4-6", Tokens{Cache1h: milhao}, false, 6.00, true},
		{"sonnet 5 com sufixo de data", "claude-sonnet-5-20260901", Tokens{Input: milhao}, false, 2.00, true},
		{"haiku 5.5 cache read", "claude-haiku-5-5", Tokens{CacheRead: milhao}, false, 0.01, true},
		{"haiku 4.5 todos os contadores", "claude-haiku-4-5-20251001",
			Tokens{Input: 1000, Cache5m: 2000, Cache1h: 3000, CacheRead: 4000, Output: 5000}, false,
			(1000*1.00 + 2000*1.25 + 3000*2.00 + 4000*0.10 + 5000*5.00) / milhao, true},
		{"todos os contadores em fast mode", "claude-opus-5-5",
			Tokens{Input: 100, Cache5m: 200, Cache1h: 300, CacheRead: 400, Output: 500}, true,
			2 * (100*4.00 + 200*5.00 + 300*8.00 + 400*0.20 + 500*20.00) / milhao, true},
		{"zero tokens custa zero", "claude-opus-5-5", Tokens{}, false, 0, true},
		{"modelo <synthetic>", "<synthetic>", Tokens{Input: milhao, Output: milhao}, false, 0, false},
		{"modelo desconhecido", "gpt-qualquer", Tokens{Input: milhao}, false, 0, false},
		{"modelo vazio", "", Tokens{Input: milhao}, false, 0, false},
		{"prefixo sem fronteira de segmento", "claude-opus-50", Tokens{Input: milhao}, false, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			usd, known := Cost(tt.model, tt.tokens, tt.fast)
			if known != tt.wantKnown {
				t.Fatalf("Cost(%q) known = %t, queria %t", tt.model, known, tt.wantKnown)
			}
			if math.Abs(usd-tt.wantUSD) > 1e-9 {
				t.Errorf("Cost(%q) usd = %.10f, queria %.10f", tt.model, usd, tt.wantUSD)
			}
		})
	}
}
