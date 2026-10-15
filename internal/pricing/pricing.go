// Package pricing calcula o custo estimado, em USD, de uma chamada ao modelo
// a partir dos tokens consumidos, usando a tabela de preço de lista da API.
package pricing

import "sort"

// Tokens agrupa os contadores de tokens de uma chamada (ou de uma soma delas).
type Tokens struct {
	Input, Cache5m, Cache1h, CacheRead, Output int64
}

// preco guarda valores em USD por milhão de tokens.
type preco struct {
	input, output, cacheRead float64
}

// Multiplicadores sobre o preço de input para escrita em cache e o fator do
// fast mode, conforme o design.
const (
	fatorCache5m = 1.25
	fatorCache1h = 2.0
	fatorFast    = 2.0
	porMilhao    = 1_000_000.0
)

// tabela de preços (preço de lista da API, consultado em 2026-10-15).
// A ordem aqui não importa: as linhas são ordenadas do prefixo mais longo
// (mais específico) para o mais curto em init.
var tabela = map[string]preco{
	"claude-fable-5-1":  {10.00, 50.00, 0.25},
	"claude-mythos-5-1": {10.00, 50.00, 0.25},
	"claude-fable-5":    {10.00, 50.00, 1.00},
	"claude-mythos-5":   {10.00, 50.00, 1.00},
	"claude-opus-5-5":   {4.00, 20.00, 0.20},
	"claude-opus-5":     {5.00, 25.00, 0.50},
	"claude-opus-4-8":   {5.00, 25.00, 0.50},
	"claude-opus-4-7":   {5.00, 25.00, 0.50},
	"claude-opus-4-6":   {5.00, 25.00, 0.50},
	"claude-sonnet-5-5": {2.00, 10.00, 0.20},
	// O cache read de claude-sonnet-5 é aproximação (ver design).
	"claude-sonnet-5":   {2.00, 10.00, 0.20},
	"claude-sonnet-4-6": {3.00, 15.00, 0.30},
	// Haiku 5.5 usa só a faixa de prompt ≤ 100K; aplicar o mesmo preço a
	// prompts > 100K é aproximação.
	"claude-haiku-5-5": {0.10, 0.50, 0.01},
	"claude-haiku-4-5": {1.00, 5.00, 0.10},
}

// prefixos lista as chaves da tabela do mais longo para o mais curto.
var prefixos []string

func init() {
	for p := range tabela {
		prefixos = append(prefixos, p)
	}
	sort.Slice(prefixos, func(i, j int) bool {
		if len(prefixos[i]) != len(prefixos[j]) {
			return len(prefixos[i]) > len(prefixos[j])
		}
		return prefixos[i] < prefixos[j]
	})
}

// buscar devolve o preço do prefixo mais específico que casa com o modelo.
// O prefixo só casa em fronteira de segmento: depois dele deve vir o fim do
// ID, um "-" (sufixo de data ou versão) ou um "[" (como em "[1m]"). Assim
// "claude-opus-5" não casa com um hipotético "claude-opus-50".
func buscar(model string) (preco, bool) {
	for _, p := range prefixos {
		if len(model) < len(p) || model[:len(p)] != p {
			continue
		}
		if len(model) == len(p) || model[len(p)] == '-' || model[len(p)] == '[' {
			return tabela[p], true
		}
	}
	return preco{}, false
}

// Cost devolve o custo estimado em USD; known=false quando o modelo não tem
// preço na tabela (inclusive "<synthetic>"), e nesse caso usd é 0.
func Cost(model string, t Tokens, fast bool) (usd float64, known bool) {
	pr, ok := buscar(model)
	if !ok {
		return 0, false
	}
	usd = (float64(t.Input)*pr.input +
		float64(t.Cache5m)*pr.input*fatorCache5m +
		float64(t.Cache1h)*pr.input*fatorCache1h +
		float64(t.CacheRead)*pr.cacheRead +
		float64(t.Output)*pr.output) / porMilhao
	if fast {
		usd *= fatorFast
	}
	return usd, true
}
