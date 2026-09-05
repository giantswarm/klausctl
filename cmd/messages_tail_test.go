package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/giantswarm/klausctl/pkg/mcpclient"
)

const tailTestEnvelope = `{"messages":[` +
	`{"role":"user","content":"one"},` +
	`{"role":"assistant","content":"two"},` +
	`{"role":"user","content":"three"},` +
	`{"role":"assistant","content":"four"}` +
	`],"total":4}`

func TestMessagesOptsFromFlags(t *testing.T) {
	restore := func() { messagesTail, messagesLimit, messagesMaxChars = 0, 0, 0 }
	t.Cleanup(restore)

	tests := []struct {
		name                  string
		tail, limit, maxChars int
		want                  *mcpclient.MessagesOpts
		wantErr               bool
	}{
		{name: "defaults pass through", want: nil},
		{name: "tail", tail: 3, want: &mcpclient.MessagesOpts{Tail: true, Limit: 3}},
		{name: "limit", limit: 20, want: &mcpclient.MessagesOpts{Limit: 20}},
		{name: "max-chars", maxChars: 500, want: &mcpclient.MessagesOpts{MaxChars: 500}},
		{name: "tail with max-chars", tail: 3, maxChars: 500, want: &mcpclient.MessagesOpts{Tail: true, Limit: 3, MaxChars: 500}},
		{name: "tail and limit conflict", tail: 3, limit: 5, wantErr: true},
		{name: "negative tail", tail: -1, wantErr: true},
		{name: "negative limit", limit: -1, wantErr: true},
		{name: "negative max-chars", maxChars: -1, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			restore()
			messagesTail, messagesLimit, messagesMaxChars = tt.tail, tt.limit, tt.maxChars

			got, err := messagesOptsFromFlags()
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if (got == nil) != (tt.want == nil) || (got != nil && *got != *tt.want) {
				t.Errorf("opts = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestRenderMessages_TailText(t *testing.T) {
	colorEnabled = false
	t.Cleanup(func() { colorEnabled = detectColor() })

	opts := &mcpclient.MessagesOpts{Tail: true, Limit: 2}
	result := mcpclient.ShapeMessages(mcp.NewToolResultText(tailTestEnvelope), opts)

	messagesOutput = outputText
	var buf bytes.Buffer
	if err := renderMessages(&buf, testInstanceDev, result, opts); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	for _, want := range []string{"Messages: 4 (showing last 2)", "[user]\nthree", "[assistant]\nfour"} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q\ngot:\n%s", want, output)
		}
	}
	for _, unwanted := range []string{"\none\n", "\ntwo\n"} {
		if strings.Contains(output, unwanted) {
			t.Errorf("output should not contain %q\ngot:\n%s", unwanted, output)
		}
	}
}

func TestRenderMessages_LimitJSON(t *testing.T) {
	opts := &mcpclient.MessagesOpts{Limit: 1, MaxChars: 2}
	result := mcpclient.ShapeMessages(mcp.NewToolResultText(tailTestEnvelope), opts)

	messagesOutput = outputJSON
	t.Cleanup(func() { messagesOutput = outputText })
	var buf bytes.Buffer
	if err := renderMessages(&buf, testInstanceDev, result, opts); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got messagesCLIResult
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, buf.String())
	}
	if got.Count != 4 {
		t.Errorf("count = %d, want the full total 4", got.Count)
	}
	if len(got.Messages) != 1 || got.Messages[0].Content != "on ...[truncated 1 chars]" {
		t.Errorf("messages = %+v, want one truncated message", got.Messages)
	}
}

func TestSliceNote(t *testing.T) {
	tests := []struct {
		name string
		opts *mcpclient.MessagesOpts
		want string
	}{
		{name: "nil", opts: nil, want: ""},
		{name: "max-chars only", opts: &mcpclient.MessagesOpts{MaxChars: 10}, want: ""},
		{name: "tail", opts: &mcpclient.MessagesOpts{Tail: true, Limit: 3}, want: "showing last 3"},
		{name: "limit", opts: &mcpclient.MessagesOpts{Limit: 20}, want: "showing first 20"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sliceNote(tt.opts); got != tt.want {
				t.Errorf("sliceNote = %q, want %q", got, tt.want)
			}
		})
	}
}
