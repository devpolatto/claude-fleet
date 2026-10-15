package transcript

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SubagentFile descreve o transcript de um subagente de uma sessão.
type SubagentFile struct {
	AgentID        string // extraído de agent-<id>.jsonl
	TranscriptPath string // caminho absoluto do .jsonl
	Type           string // agentType do .meta.json; vazio se ausente ou inválido
	ToolUseID      string // toolUseId do .meta.json; vazio se ausente ou inválido
}

// subagentMeta é a lista branca do agent-<id>.meta.json: só os campos usados
// são decodificados. Os demais (spawnDepth, requestShape, ...) são ignorados
// pelo decodificador e nunca chegam à memória do programa.
type subagentMeta struct {
	AgentType string `json:"agentType"`
	ToolUseID string `json:"toolUseId"`
}

const (
	subagentPrefix = "agent-"
	subagentExt    = ".jsonl"
)

// SubagentsOf recebe o transcript da sessão principal (<dir>/<session_id>.jsonl)
// e lista <dir>/<session_id>/subagents/agent-*.jsonl, ordenados por AgentID.
//
// Para cada transcript, lê agentType e toolUseId do
// agent-<id>.meta.json vizinho. Se o .meta.json não existir ou não for JSON
// válido, a entrada aparece mesmo assim, com esses campos vazios.
//
// Devolve nil quando o diretório subagents não existe ou não pode ser lido, e
// também quando não há nenhum agent-*.jsonl: quem chama só precisa de len().
// Arquivos fora do padrão agent-<id>.jsonl e subdiretórios são ignorados.
func SubagentsOf(transcriptPath string) []SubagentFile {
	if !strings.HasSuffix(transcriptPath, subagentExt) {
		return nil
	}
	dir := filepath.Join(strings.TrimSuffix(transcriptPath, subagentExt), "subagents")
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var out []SubagentFile
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, subagentPrefix) || !strings.HasSuffix(name, subagentExt) {
			continue
		}
		id := strings.TrimSuffix(strings.TrimPrefix(name, subagentPrefix), subagentExt)
		if id == "" {
			continue
		}
		sf := SubagentFile{AgentID: id, TranscriptPath: filepath.Join(dir, name)}
		if m, ok := readSubagentMeta(filepath.Join(dir, subagentPrefix+id+".meta.json")); ok {
			sf.Type, sf.ToolUseID = m.AgentType, m.ToolUseID
		}
		out = append(out, sf)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AgentID < out[j].AgentID })
	return out
}

// readSubagentMeta lê o .meta.json; ok é false se ele faltar ou for inválido.
func readSubagentMeta(path string) (subagentMeta, bool) {
	var m subagentMeta
	data, err := os.ReadFile(path)
	if err != nil {
		return m, false
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return subagentMeta{}, false
	}
	return m, true
}
