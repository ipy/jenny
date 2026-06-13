// Package agent provides the core agent loop and query engine.
package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ipy/jenny/internal/api"
)

// contentBlockStopEvent represents a content_block_stop stream event with minimal fields.
type contentBlockStopEvent struct {
	Type  string `json:"type"`
	Index int    `json:"index"`
}

// messageStopEvent represents a message_stop stream event with minimal fields.
type messageStopEvent struct {
	Type string `json:"type"`
}

// MinimalContentBlock represents a minimal content block for serialization.
// Only relevant fields based on block type are included.
// Implements json.Marshaler for custom serialization without zero-value padding.
type MinimalContentBlock struct {
	Type      string
	Thinking  string
	Signature string
	Text      string
	ID        string
	Name      string
	Input     any
}

func (m MinimalContentBlock) MarshalJSON() ([]byte, error) {
	// Build fields in order: type first, then only non-empty fields
	fields := []any{`"type":` + encodeString(m.Type)}

	switch m.Type {
	case api.BlockTypeThinking:
		if m.Thinking != "" {
			fields = append(fields, `"thinking":`+encodeString(m.Thinking))
		}
		if m.Signature != "" {
			fields = append(fields, `"signature":`+encodeString(m.Signature))
		}
	case api.BlockTypeText:
		// Always include text field even when empty (per reference format for content_block_start)
		fields = append(fields, `"text":`+encodeString(m.Text))
	case api.BlockTypeToolUse:
		fields = append(fields, `"id":`+encodeString(m.ID))
		fields = append(fields, `"name":`+encodeString(m.Name))
		if m.Input != nil {
			inputBytes, err := json.Marshal(m.Input)
			if err != nil {
				return nil, err
			}
			fields = append(fields, `"input":`+string(inputBytes))
		}
	case api.BlockTypeRedactedThinking:
		if m.Text != "" {
			fields = append(fields, `"data":`+encodeString(m.Text))
		}
	}

	return []byte("{" + joinFields(fields) + "}"), nil
}

// MinimalDelta represents a minimal delta for message_delta events.
type MinimalDelta struct {
	Type        string
	Thinking    string
	PartialJSON string
	Signature   string
	StopReason  string
	StopSeq     *string // Use *string so it can marshal as null when empty
	Text        string
}

func (m MinimalDelta) MarshalJSON() ([]byte, error) {
	var fields []any

	switch m.Type {
	case api.DeltaTypeThinking:
		fields = []any{`"type":"thinking_delta"`}
		if m.Thinking != "" {
			fields = append(fields, `"thinking":`+encodeString(m.Thinking))
		}
		if m.Signature != "" {
			fields = append(fields, `"signature":`+encodeString(m.Signature))
		}
	case api.DeltaTypeText:
		fields = []any{`"type":"text_delta"`}
		if m.Text != "" {
			fields = append(fields, `"text":`+encodeString(m.Text))
		}
	case api.DeltaTypeInputJSON:
		fields = []any{`"type":"input_json_delta"`}
		if m.PartialJSON != "" {
			fields = append(fields, `"partial_json":`+encodeString(m.PartialJSON))
		}
	case api.DeltaTypeSignature:
		fields = []any{`"type":"signature_delta"`}
		if m.Signature != "" {
			fields = append(fields, `"signature":`+encodeString(m.Signature))
		}
	case api.EventMessageDelta:
		// Reference format: delta has stop_reason/stop_sequence directly, no nested type field
		// Always include stop_reason and stop_sequence (possibly null)
		fields = []any{}
		if m.StopReason != "" {
			fields = append(fields, `"stop_reason":`+encodeString(m.StopReason))
		} else {
			fields = append(fields, `"stop_reason":null`)
		}
		if m.StopSeq != nil {
			fields = append(fields, `"stop_sequence":`+encodeString(*m.StopSeq))
		} else {
			fields = append(fields, `"stop_sequence":null`)
		}
	default:
		fields = []any{`"type":` + encodeString(m.Type)}
	}

	return []byte("{" + joinFields(fields) + "}"), nil
}

// MinimalMessage represents a minimal message for message_start events.
// Uses *string for StopReason and StopSeq so they marshal as null when empty.
type MinimalMessage struct {
	ID         string
	Type       string
	Role       string
	Model      string
	Content    any
	Usage      *StreamUsage
	StopReason *string
	StopSeq    *string
}

func (m MinimalMessage) MarshalJSON() ([]byte, error) {
	fields := []any{`"id":` + encodeString(m.ID), `"type":` + encodeString(m.Type), `"role":` + encodeString(m.Role)}

	if m.Model != "" {
		fields = append(fields, `"model":`+encodeString(m.Model))
	}

	if m.Content != nil {
		contentBytes, err := json.Marshal(m.Content)
		if err != nil {
			return nil, err
		}
		fields = append(fields, `"content":`+string(contentBytes))
	}

	// Always include stop_reason and stop_sequence (possibly null per reference format)
	if m.StopReason != nil {
		fields = append(fields, `"stop_reason":`+encodeString(*m.StopReason))
	} else {
		fields = append(fields, `"stop_reason":null`)
	}
	if m.StopSeq != nil {
		fields = append(fields, `"stop_sequence":`+encodeString(*m.StopSeq))
	} else {
		fields = append(fields, `"stop_sequence":null`)
	}

	if m.Usage != nil {
		usageBytes, err := json.Marshal(m.Usage)
		if err != nil {
			return nil, err
		}
		fields = append(fields, `"usage":`+string(usageBytes))
	}

	return []byte("{" + joinFields(fields) + "}"), nil
}

// StreamUsage represents a minimal usage object for stream events.
// Field order matches the reference format: input_tokens, cache_creation_input_tokens,
// cache_read_input_tokens, output_tokens, service_tier.
type StreamUsage struct {
	InputTokens              int    `json:"input_tokens"`
	CacheCreationInputTokens int    `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int    `json:"cache_read_input_tokens"`
	OutputTokens             int    `json:"output_tokens"`
	ServiceTier              string `json:"service_tier"`
}

func encodeString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func joinFields(fields []any) string {
	var result strings.Builder
	for i, f := range fields {
		if i > 0 {
			result.WriteString(",")
		}
		result.WriteString(fmt.Sprintf("%v", f))
	}
	return result.String()
}

// TransformStreamEvent transforms an SDK stream event to a minimal JSON representation.
// This ensures only relevant fields are serialized without zero-value padding.
func TransformStreamEvent(event any) (json.RawMessage, error) {
	if anthropicEvent, ok := event.(api.AnthropicStreamEvent); ok {
		switch anthropicEvent.Type {
		case api.EventMessageStart:
			return transformMessageStart(anthropicEvent)
		case api.EventContentBlockStart:
			return transformContentBlockStart(anthropicEvent)
		case api.EventContentBlockDelta:
			return transformContentBlockDelta(anthropicEvent)
		case api.EventContentBlockStop:
			return transformContentBlockStop(anthropicEvent)
		case api.EventMessageDelta:
			return transformMessageDelta(anthropicEvent)
		case api.EventMessageStop:
			return transformMessageStop(anthropicEvent)
		}
	}
	// Fallback: marshal as-is but this may include zero-value fields
	return json.Marshal(event)
}

func transformMessageStart(e api.AnthropicStreamEvent) (json.RawMessage, error) {
	usage := &StreamUsage{
		InputTokens:              e.Message.Usage.InputTokens,
		CacheCreationInputTokens: e.Message.Usage.CacheCreationInputTokens,
		CacheReadInputTokens:     e.Message.Usage.CacheReadInputTokens,
		OutputTokens:             e.Message.Usage.OutputTokens,
		ServiceTier:              "standard",
	}

	msg := struct {
		Type    string         `json:"type"`
		Message MinimalMessage `json:"message"`
	}{
		Type: api.EventMessageStart,
		Message: MinimalMessage{
			ID:      e.Message.ID,
			Type:    "message",
			Role:    api.RoleAssistant,
			Model:   e.Message.Model,
			Content: []any{},
			Usage:   usage,
		},
	}
	// Set StopReason as *string - nil means null, pointer means value
	if e.Message.StopReason != "" {
		msg.Message.StopReason = &e.Message.StopReason
	}
	// Set StopSeq as *string - nil means null, pointer means value
	if e.Message.StopSequence != "" {
		msg.Message.StopSeq = &e.Message.StopSequence
	}
	return json.Marshal(msg)
}

func transformContentBlockStart(e api.AnthropicStreamEvent) (json.RawMessage, error) {
	cb := MinimalContentBlock{Type: e.ContentBlock.Type}

	switch e.ContentBlock.Type {
	case api.BlockTypeThinking:
		cb.Thinking = e.ContentBlock.Thinking
		cb.Signature = e.ContentBlock.Signature
	case api.BlockTypeText:
		cb.Text = e.ContentBlock.Text
	case api.BlockTypeToolUse:
		cb.ID = e.ContentBlock.ID
		cb.Name = e.ContentBlock.Name
		if e.ContentBlock.Input != nil {
			cb.Input = e.ContentBlock.Input
		}
	}

	msg := struct {
		Type         string              `json:"type"`
		Index        int                 `json:"index"`
		ContentBlock MinimalContentBlock `json:"content_block"`
	}{
		Type:         api.EventContentBlockStart,
		Index:        e.Index,
		ContentBlock: cb,
	}
	return json.Marshal(msg)
}

func transformContentBlockDelta(e api.AnthropicStreamEvent) (json.RawMessage, error) {
	delta := MinimalDelta{Type: e.Delta.Type}

	switch e.Delta.Type {
	case api.DeltaTypeThinking:
		delta.Thinking = e.Delta.Thinking
		delta.Signature = e.Delta.Signature
	case api.DeltaTypeText:
		delta.Text = e.Delta.Text
	case api.DeltaTypeInputJSON:
		delta.PartialJSON = e.Delta.PartialJSON
	case api.DeltaTypeSignature:
		delta.Signature = e.Delta.Signature
	}

	msg := struct {
		Type  string       `json:"type"`
		Index int          `json:"index"`
		Delta MinimalDelta `json:"delta"`
	}{
		Type:  api.EventContentBlockDelta,
		Index: e.Index,
		Delta: delta,
	}
	return json.Marshal(msg)
}

func transformContentBlockStop(e api.AnthropicStreamEvent) (json.RawMessage, error) {
	event := contentBlockStopEvent{
		Type:  api.EventContentBlockStop,
		Index: e.Index,
	}
	return json.Marshal(event)
}

func transformMessageDelta(e api.AnthropicStreamEvent) (json.RawMessage, error) {
	delta := MinimalDelta{Type: api.EventMessageDelta}
	if e.Delta.StopReason != "" {
		delta.StopReason = e.Delta.StopReason
	}
	// Use pointer for StopSeq so it marshals as null when empty
	if e.Delta.StopSequence != "" {
		delta.StopSeq = &e.Delta.StopSequence
	}

	msg := struct {
		Type  string       `json:"type"`
		Delta MinimalDelta `json:"delta"`
		Usage *StreamUsage `json:"usage,omitempty"`
	}{
		Type:  api.EventMessageDelta,
		Delta: delta,
	}

	// Add usage if present
	// Field order: input_tokens, cache_creation_input_tokens, cache_read_input_tokens, output_tokens
	if e.Usage != nil && (e.Usage.InputTokens > 0 || e.Usage.OutputTokens > 0 ||
		e.Usage.CacheReadInputTokens > 0 || e.Usage.CacheCreationInputTokens > 0) {
		msg.Usage = &StreamUsage{
			InputTokens:              e.Usage.InputTokens,
			CacheCreationInputTokens: e.Usage.CacheCreationInputTokens,
			CacheReadInputTokens:     e.Usage.CacheReadInputTokens,
			OutputTokens:             e.Usage.OutputTokens,
			ServiceTier:              "standard",
		}
	}

	return json.Marshal(msg)
}

func transformMessageStop(e api.AnthropicStreamEvent) (json.RawMessage, error) {
	event := messageStopEvent{
		Type: api.EventMessageStop,
	}
	return json.Marshal(event)
}
