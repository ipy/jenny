package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/ipy/jenny/internal/api"
)

const (
	toolSoftThreshold        = 3
	toolHardThreshold        = 6
	maxToolCallEntries       = 100
	maxWrittenPaths          = 200
	textHashWindowSize       = 10
	assistantTextRepeatScore = -5
	toolSoftRepeatScore      = -2
	toolHardRepeatScore      = -3
	minCycleLength           = 2
	maxCycleLength           = 5
	minCycleRepetitions      = 3
)

// ToolAction describes how the engine should handle a tool call.
type ToolAction int

const (
	ToolActionExecute ToolAction = iota
	ToolActionSkipDuplicate
	ToolActionBlock
)

// ToolCallDecision is the Loop Guard verdict for one tool_use block.
type ToolCallDecision struct {
	Action    ToolAction
	ToolIndex int
	Warning   string
	BlockMsg  string
}

// LoopGuard detects repeated tool calls and assistant text patterns.
type LoopGuard struct {
	toolEntries    map[string]*toolCallEntry
	toolLRU        []string
	textHashes     []string
	writtenPaths   map[string]bool
	writtenPathLRU []string
}

type toolCallEntry struct {
	count    int
	toolName string
	filePath string
}

// NewLoopGuard creates a Loop Guard with empty state.
func NewLoopGuard() *LoopGuard {
	return &LoopGuard{
		toolEntries:  make(map[string]*toolCallEntry),
		textHashes:   make([]string, 0, textHashWindowSize),
		writtenPaths: make(map[string]bool),
	}
}

// EvaluateToolCalls analyzes tool use blocks before execution.
// Every tool call is always executed — no batch-internal deduplication is performed
// because even read-only tools may depend on side effects of other tools in the
// same batch (e.g., read after bash in a single response).
func (lg *LoopGuard) EvaluateToolCalls(toolUseBlocks []api.ToolUseBlock) []ToolCallDecision {
	decisions := make([]ToolCallDecision, len(toolUseBlocks))

	for i, block := range toolUseBlocks {
		key := toolCallKey(block.Name, block.Input)
		entry := lg.getOrCreateEntry(key, block.Name, block.Input)
		entry.count++

		switch {
		case entry.count >= toolHardThreshold:
			decisions[i] = ToolCallDecision{
				Action:   ToolActionBlock,
				BlockMsg: fmt.Sprintf("Loop Guard: tool %q with identical arguments has been called %d times. Try a different approach.", block.Name, entry.count),
			}
		case entry.count >= toolSoftThreshold:
			decisions[i] = ToolCallDecision{
				Action:  ToolActionExecute,
				Warning: softToolWarning(block.Name, entry.count),
			}
		default:
			decisions[i] = ToolCallDecision{Action: ToolActionExecute}
		}
		lg.touchLRU(key)
	}

	return decisions
}

// ToolTurnScore returns negative score adjustments and signals for tool decisions.
func (lg *LoopGuard) ToolTurnScore(decisions []ToolCallDecision) TurnScore {
	ts := TurnScore{}
	for _, d := range decisions {
		switch d.Action {
		case ToolActionBlock:
			ts.Score += toolHardRepeatScore
			ts.Signals = append(ts.Signals, "blocked_tool_repeat")
		case ToolActionExecute:
			if d.Warning != "" {
				ts.Score += toolSoftRepeatScore
				ts.Signals = append(ts.Signals, "repeated_tool_call")
			}
		}
	}
	return ts
}

// RecordAssistantText records assistant output and detects text repetition.
func (lg *LoopGuard) RecordAssistantText(text string) (score int, signals []string) {
	if strings.TrimSpace(text) == "" {
		return 0, nil
	}

	hash := normalizeTextHash(text)
	lg.textHashes = append(lg.textHashes, hash)
	if len(lg.textHashes) > textHashWindowSize {
		lg.textHashes = lg.textHashes[len(lg.textHashes)-textHashWindowSize:]
	}

	if detectDirectRepeat(lg.textHashes) {
		return assistantTextRepeatScore, []string{"direct_text_repeat"}
	}
	if detectCyclicRepeat(lg.textHashes) {
		return assistantTextRepeatScore, []string{"cyclic_text_repeat"}
	}
	return 0, nil
}

// NotifyFileWrite resets the counter for tool calls targeting a newly written path.
func (lg *LoopGuard) NotifyFileWrite(path string) {
	if path == "" {
		return
	}
	if lg.writtenPaths[path] {
		return
	}
	if len(lg.writtenPaths) >= maxWrittenPaths {
		oldest := lg.writtenPathLRU[0]
		copy(lg.writtenPathLRU, lg.writtenPathLRU[1:])
		lg.writtenPathLRU = lg.writtenPathLRU[:len(lg.writtenPathLRU)-1]
		delete(lg.writtenPaths, oldest)
	}
	lg.writtenPaths[path] = true
	lg.writtenPathLRU = append(lg.writtenPathLRU, path)
	for _, entry := range lg.toolEntries {
		if entry.filePath == path {
			entry.count = 0
		}
	}
}

// Reset clears all Loop Guard state.
func (lg *LoopGuard) Reset() {
	lg.toolEntries = make(map[string]*toolCallEntry)
	lg.toolLRU = nil
	lg.textHashes = lg.textHashes[:0]
	lg.writtenPaths = make(map[string]bool)
	lg.writtenPathLRU = nil
}

func (lg *LoopGuard) getOrCreateEntry(key, toolName string, input map[string]any) *toolCallEntry {
	if entry, ok := lg.toolEntries[key]; ok {
		return entry
	}
	if len(lg.toolEntries) >= maxToolCallEntries {
		lg.evictOldest()
	}
	entry := &toolCallEntry{
		toolName: toolName,
		filePath: extractWritePath(toolName, input),
	}
	lg.toolEntries[key] = entry
	return entry
}

func (lg *LoopGuard) touchLRU(key string) {
	for i, k := range lg.toolLRU {
		if k == key {
			copy(lg.toolLRU[i:], lg.toolLRU[i+1:])
			lg.toolLRU = lg.toolLRU[:len(lg.toolLRU)-1]
			break
		}
	}
	lg.toolLRU = append(lg.toolLRU, key)
}

func (lg *LoopGuard) evictOldest() {
	if len(lg.toolLRU) == 0 {
		return
	}
	oldest := lg.toolLRU[0]
	copy(lg.toolLRU, lg.toolLRU[1:])
	lg.toolLRU = lg.toolLRU[:len(lg.toolLRU)-1]
	delete(lg.toolEntries, oldest)
}

func toolCallKey(toolName string, input map[string]any) string {
	return toolName + ":" + canonicalArgsHash(toolName, input)
}

func canonicalArgsHash(toolName string, input map[string]any) string {
	_ = toolName
	normalized := normalizeArgsMap(input)
	data, err := json.Marshal(normalized)
	if err != nil {
		data = []byte("{}")
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:8])
}

func normalizeArgsMap(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	keys := make([]string, 0, len(input))
	for k := range input {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := make(map[string]any, len(input))
	for _, k := range keys {
		out[k] = normalizeArgValue(input[k])
	}
	return out
}

func normalizeArgValue(v any) any {
	switch val := v.(type) {
	case string:
		return strings.Join(strings.Fields(val), " ")
	case map[string]any:
		return normalizeArgsMap(val)
	case []any:
		norm := make([]any, len(val))
		for i, item := range val {
			norm[i] = normalizeArgValue(item)
		}
		return norm
	default:
		return v
	}
}

func extractWritePath(toolName string, input map[string]any) string {
	switch toolName {
	case "write", "edit", "notebook_edit":
		return extractStringField(input, "file_path")
	default:
		return ""
	}
}

func normalizeTextHash(text string) string {
	normalized := strings.ToLower(strings.Join(strings.Fields(text), " "))
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:8])
}

func detectDirectRepeat(hashes []string) bool {
	n := len(hashes)
	if n < 3 {
		return false
	}
	h := hashes[n-1]
	return hashes[n-2] == h && hashes[n-3] == h
}

func detectCyclicRepeat(hashes []string) bool {
	n := len(hashes)
	for cycleLen := minCycleLength; cycleLen <= maxCycleLength; cycleLen++ {
		needed := cycleLen * minCycleRepetitions
		if n < needed {
			continue
		}
		pattern := hashes[n-cycleLen : n]
		match := true
		for rep := 1; rep < minCycleRepetitions; rep++ {
			start := n - cycleLen*(rep+1)
			segment := hashes[start : start+cycleLen]
			for i := 0; i < cycleLen; i++ {
				if segment[i] != pattern[i] {
					match = false
					break
				}
			}
			if !match {
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func softToolWarning(toolName string, count int) string {
	return fmt.Sprintf(`<system-reminder>
LOOP GUARD: You have called %q with identical arguments %d times. Repeating the same tool call is unlikely to produce different results. Consider a different file, command, or strategy.
</system-reminder>`, toolName, count)
}
