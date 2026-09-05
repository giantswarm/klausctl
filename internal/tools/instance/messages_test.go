package instance

import (
	"context"
	"strings"
	"testing"

	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/giantswarm/klausctl/pkg/mcpclient"
)

// testInstance is the instance name used by the messages tests.
const testInstance = "inst"

func TestMessagesOptsFromReq(t *testing.T) {
	tests := []struct {
		name    string
		args    map[string]any
		want    *mcpclient.MessagesOpts
		wantErr bool
	}{
		{name: "no options", args: map[string]any{paramName: testInstance}, want: nil},
		{name: "negative offset clamps to unset", args: map[string]any{paramOffset: -3.0}, want: nil},
		{name: "offset and types", args: map[string]any{paramOffset: 5.0, paramTypes: "assistant"}, want: &mcpclient.MessagesOpts{Offset: 5, Types: "assistant"}},
		{name: "tail limit maxChars", args: map[string]any{paramTail: true, paramLimit: 3.0, paramMaxChars: 500.0}, want: &mcpclient.MessagesOpts{Tail: true, Limit: 3, MaxChars: 500}},
		{name: "tail alone", args: map[string]any{paramTail: true}, want: &mcpclient.MessagesOpts{Tail: true}},
		{name: "negative limit", args: map[string]any{paramLimit: -1.0}, wantErr: true},
		{name: "fractional limit", args: map[string]any{paramLimit: 2.5}, wantErr: true},
		{name: "negative maxChars", args: map[string]any{paramMaxChars: -10.0}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := messagesOptsFromReq(callToolRequest(tt.args))
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

func TestHandleMessagesRejectsNegativeLimit(t *testing.T) {
	sc := testServerContext(t)
	// Validation runs before the instance lookup, so the error must name the
	// parameter rather than the missing instance.
	req := callToolRequest(map[string]any{paramName: testInstance, paramLimit: -5.0})
	result, err := handleMessages(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertIsError(t, result)
	if text := extractResultText(t, result); !strings.Contains(text, paramLimit) {
		t.Errorf("error should name the limit parameter, got %q", text)
	}
}

func TestKlausMessagesToolInputs(t *testing.T) {
	sc := testServerContext(t)
	srv := mcpserver.NewMCPServer("test", "1.0.0", mcpserver.WithToolCapabilities(false))
	registerMessages(srv, sc)

	tool, ok := srv.ListTools()["klaus_messages"]
	if !ok {
		t.Fatal("klaus_messages is not registered")
	}
	for _, p := range []string{paramName, paramOffset, paramLimit, paramTail, paramMaxChars, paramTypes, "follow"} {
		if _, ok := tool.Tool.InputSchema.Properties[p]; !ok {
			t.Errorf("klaus_messages is missing input %q", p)
		}
	}
	for _, want := range []string{paramTail, paramMaxChars, "next_offset"} {
		if !strings.Contains(tool.Tool.Description, want) {
			t.Errorf("description should mention %q: %s", want, tool.Tool.Description)
		}
	}
}
