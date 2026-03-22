// Package rawmsg converts the RawMessagesInfo format returned by the klaus
// agent's messages MCP tool into simple role/content pairs.
//
// The agent now returns messages as json.RawMessage items with fields like
// type, subtype, text, tool_name, and timestamp instead of the old
// role/content format.
package rawmsg

import (
	"encoding/json"
	"fmt"
)

// Message is a simplified role/content pair suitable for display.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// RawResponse is the JSON payload returned by the agent's messages MCP tool
// in the new RawMessagesInfo format.
type RawResponse struct {
	Status   string            `json:"status"`
	Total    int               `json:"total"`
	Messages []json.RawMessage `json:"messages"`
}

// rawItem represents a single raw message item from the agent.
type rawItem struct {
	Type     string `json:"type"`
	Subtype  string `json:"subtype"`
	Text     string `json:"text"`
	ToolName string `json:"tool_name"`
}

// Parse attempts to parse a JSON string as a RawResponse and convert its
// messages to role/content pairs. It returns the status, total count, and
// converted messages. If the input does not match the raw format (e.g. it
// lacks the "total" field), ok is false and the caller should fall back to
// the legacy format.
func Parse(data string) (status string, total int, messages []Message, ok bool) {
	var raw RawResponse
	if err := json.Unmarshal([]byte(data), &raw); err != nil {
		return "", 0, nil, false
	}
	// Distinguish from the legacy format: the raw format always has "total".
	if raw.Status == "" {
		return "", 0, nil, false
	}
	// Check if this looks like the raw format by trying to parse the first
	// message. Legacy format has "role"/"content" keys; raw format has "type".
	if len(raw.Messages) > 0 {
		var probe struct {
			Type string `json:"type"`
			Role string `json:"role"`
		}
		if json.Unmarshal(raw.Messages[0], &probe) == nil && probe.Role != "" && probe.Type == "" {
			// Legacy format — let the caller handle it.
			return "", 0, nil, false
		}
	}

	msgs := make([]Message, 0, len(raw.Messages))
	for _, m := range raw.Messages {
		msgs = append(msgs, Convert(m))
	}
	return raw.Status, raw.Total, msgs, true
}

// Convert converts a single json.RawMessage into a role/content Message.
func Convert(data json.RawMessage) Message {
	var item rawItem
	if err := json.Unmarshal(data, &item); err != nil {
		return Message{Role: "unknown", Content: string(data)}
	}
	return convertItem(item)
}

func convertItem(item rawItem) Message {
	switch item.Type {
	case "assistant":
		switch item.Subtype {
		case "tool_use":
			name := item.ToolName
			if name == "" {
				name = "unknown"
			}
			content := fmt.Sprintf("[tool_use: %s]", name)
			if item.Text != "" {
				content = fmt.Sprintf("[tool_use: %s] %s", name, item.Text)
			}
			return Message{Role: "assistant", Content: content}
		default:
			return Message{Role: "assistant", Content: item.Text}
		}

	case "tool_result":
		content := item.Text
		if len(content) > 500 {
			content = content[:500] + "…"
		}
		return Message{Role: "tool", Content: content}

	case "user":
		return Message{Role: "user", Content: item.Text}

	case "system":
		return Message{Role: "system", Content: item.Text}

	default:
		content := item.Text
		if content == "" {
			content = string("(empty)")
		}
		return Message{Role: item.Type, Content: content}
	}
}
