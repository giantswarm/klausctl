package mcpclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/mark3labs/mcp-go/mcp"
)

// DefaultTailLimit is the number of messages returned when Tail is set
// without an explicit Limit.
const DefaultTailLimit = 10

const (
	keyMessages   = "messages"
	keyMetadata   = "metadata"
	keyTotal      = "total"
	keyMatched    = "matched"
	keyReturned   = "returned"
	keyNextOffset = "next_offset"
	keyTruncated  = "truncated"
)

// envelopeKeyOrder fixes the position of the well-known fields in a shaped
// envelope; any other field of the agent's response follows alphabetically.
var envelopeKeyOrder = []string{keyMessages, keyMetadata, keyTotal, keyMatched, keyReturned, keyNextOffset, keyTruncated}

// structuralKeys name fields that identify or route a message rather than
// carry its body; MaxChars never truncates them.
var structuralKeys = map[string]bool{
	"role":         true,
	"type":         true,
	"subtype":      true,
	"id":           true,
	"name":         true,
	"tool_call_id": true,
	"tool_use_id":  true,
	"session_id":   true,
	"model":        true,
}

// shapesLocally reports whether opts asks for work the agent does not do
// itself. The agent's messages tool honours offset only, so type filtering,
// tail/limit slicing and truncation happen in klausctl.
func (o *MessagesOpts) shapesLocally() bool {
	return o != nil && (o.Types != "" || o.Limit > 0 || o.Tail || o.MaxChars > 0)
}

// ShapeMessages applies the klausctl-side parts of opts (Types, Limit, Tail,
// MaxChars) to the envelope returned by the agent's messages tool. The result
// is returned as-is when opts needs no local shaping, when the result is an
// error, or when its text is not a {messages, ...} envelope.
func ShapeMessages(result *mcp.CallToolResult, opts *MessagesOpts) *mcp.CallToolResult {
	if result == nil || result.IsError || !opts.shapesLocally() {
		return result
	}
	shaped, ok := ShapeMessagesText(ExtractText(result), opts)
	if !ok {
		return result
	}
	return mcp.NewToolResultText(shaped)
}

// ShapeMessagesText reshapes the JSON text of a messages envelope. It returns
// the input unchanged with ok=false when opts needs no local shaping or when
// text is not a JSON object with a messages array.
//
// The output keeps every top-level field of the input, replaces messages with
// the selected slice and sets:
//   - total: full message count as reported by the agent (Offset+len(messages) when absent)
//   - matched: messages in the fetched window that pass the Types filter
//   - returned: number of messages in the slice
//   - next_offset: the offset that fetches the messages after the slice
//   - truncated: whether MaxChars cut any string (present only when MaxChars > 0)
func ShapeMessagesText(text string, opts *MessagesOpts) (string, bool) {
	if !opts.shapesLocally() {
		return text, false
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text), &fields); err != nil || fields == nil {
		return text, false
	}
	rawList, ok := fields[keyMessages]
	if !ok {
		return text, false
	}
	var msgs []json.RawMessage
	if err := json.Unmarshal(rawList, &msgs); err != nil {
		return text, false
	}

	offset := max(opts.Offset, 0)
	total := offset + len(msgs)
	if raw, ok := fields[keyTotal]; ok {
		var n int
		if err := json.Unmarshal(raw, &n); err == nil && n >= 0 {
			total = n
		}
	}

	// Indices into msgs of the messages that pass the Types filter.
	typeSet := parseTypes(opts.Types)
	idx := make([]int, 0, len(msgs))
	for i, m := range msgs {
		if len(typeSet) == 0 || messageMatches(m, typeSet) {
			idx = append(idx, i)
		}
	}
	matched := len(idx)

	limit := opts.Limit
	if opts.Tail && limit <= 0 {
		limit = DefaultTailLimit
	}
	if limit > 0 && len(idx) > limit {
		if opts.Tail {
			idx = idx[len(idx)-limit:]
		} else {
			idx = idx[:limit]
		}
	}

	nextOffset := offset + len(msgs)
	if len(idx) > 0 {
		nextOffset = offset + idx[len(idx)-1] + 1
	}

	selected := make([]json.RawMessage, 0, len(idx))
	truncated := false
	for _, i := range idx {
		m := msgs[i]
		if opts.MaxChars > 0 {
			var cut bool
			m, cut = truncateMessage(m, opts.MaxChars)
			truncated = truncated || cut
		}
		selected = append(selected, m)
	}

	out := make(map[string]json.RawMessage, len(fields)+5)
	for k, v := range fields {
		out[k] = v
	}
	out[keyMessages] = mustMarshal(selected)
	out[keyTotal] = mustMarshal(total)
	out[keyMatched] = mustMarshal(matched)
	out[keyReturned] = mustMarshal(len(selected))
	out[keyNextOffset] = mustMarshal(nextOffset)
	if opts.MaxChars > 0 {
		out[keyTruncated] = mustMarshal(truncated)
	}

	return string(marshalOrdered(out, envelopeKeyOrder)), true
}

// parseTypes splits a comma-separated types list into a lower-cased set.
func parseTypes(types string) map[string]bool {
	set := make(map[string]bool)
	for _, t := range strings.Split(types, ",") {
		if t = strings.ToLower(strings.TrimSpace(t)); t != "" {
			set[t] = true
		}
	}
	return set
}

// messageMatches reports whether a message's role (OpenAI format) or type
// (stream-json envelope) is in set.
func messageMatches(raw json.RawMessage, set map[string]bool) bool {
	var probe struct {
		Role string `json:"role"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return false
	}
	return set[strings.ToLower(probe.Role)] || set[strings.ToLower(probe.Type)]
}

// truncateMessage cuts every non-structural string inside a message down to
// maxChars runes. The raw bytes are returned untouched when nothing was cut.
func truncateMessage(raw json.RawMessage, maxChars int) (json.RawMessage, bool) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return raw, false
	}
	v, changed := truncateValue(v, "", maxChars)
	if !changed {
		return raw, false
	}
	out, err := json.Marshal(v)
	if err != nil {
		return raw, false
	}
	return out, true
}

// truncateValue walks a decoded JSON value. key is the object key the value
// sits under (inherited by array elements) and decides whether a string is
// structural.
func truncateValue(v any, key string, maxChars int) (any, bool) {
	switch t := v.(type) {
	case string:
		if structuralKeys[key] || utf8.RuneCountInString(t) <= maxChars {
			return t, false
		}
		return truncateString(t, maxChars), true
	case map[string]any:
		changed := false
		for k, val := range t {
			if nv, c := truncateValue(val, k, maxChars); c {
				t[k] = nv
				changed = true
			}
		}
		return t, changed
	case []any:
		changed := false
		for i, val := range t {
			if nv, c := truncateValue(val, key, maxChars); c {
				t[i] = nv
				changed = true
			}
		}
		return t, changed
	default:
		return v, false
	}
}

// truncateString keeps the first maxChars runes of s and appends a marker
// naming how many runes were cut.
func truncateString(s string, maxChars int) string {
	runes := []rune(s)
	return string(runes[:maxChars]) + fmt.Sprintf(" ...[truncated %d chars]", len(runes)-maxChars)
}

// mustMarshal encodes plain ints, bools and raw-message slices, none of which
// can fail to marshal.
func mustMarshal(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("mcpclient: marshalling %T: %v", v, err))
	}
	return b
}

// marshalOrdered encodes fields as a JSON object, emitting the keys in first
// (those present) before the remaining keys in alphabetical order.
func marshalOrdered(fields map[string]json.RawMessage, first []string) []byte {
	var buf bytes.Buffer
	buf.WriteByte('{')
	written := make(map[string]bool, len(fields))
	write := func(k string) {
		v, ok := fields[k]
		if !ok || written[k] {
			return
		}
		if len(written) > 0 {
			buf.WriteByte(',')
		}
		buf.Write(mustMarshal(k))
		buf.WriteByte(':')
		buf.Write(v)
		written[k] = true
	}
	for _, k := range first {
		write(k)
	}
	rest := make([]string, 0, len(fields))
	for k := range fields {
		if !written[k] {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	for _, k := range rest {
		write(k)
	}
	buf.WriteByte('}')
	return buf.Bytes()
}
