package agentclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func sseServer(lines ...string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			http.Error(w, "bad content-type", http.StatusBadRequest)
			return
		}
		if accept := r.Header.Get("Accept"); accept != "text/event-stream" {
			http.Error(w, "bad accept header", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, _ := w.(http.Flusher)
		for _, line := range lines {
			_, _ = fmt.Fprintln(w, line)
			if flusher != nil {
				flusher.Flush()
			}
		}
	}))
}

func chunkLine(content string) string {
	return fmt.Sprintf(`data: {"choices":[{"delta":{"content":"%s"}}]}`, content)
}

func collectDeltas(t *testing.T, ch <-chan CompletionDelta) ([]string, error) {
	t.Helper()
	var contents []string
	for delta := range ch {
		if delta.Err != nil {
			return contents, delta.Err
		}
		contents = append(contents, delta.Content)
	}
	return contents, nil
}

func TestStreamCompletionBasic(t *testing.T) {
	srv := sseServer(
		chunkLine("Hello"),
		chunkLine(" world"),
		"data: [DONE]",
	)
	defer srv.Close()

	ch, err := StreamCompletion(context.Background(), srv.Client(), CompletionRequest{URL: srv.URL + "/v1/chat/completions", Prompt: "test prompt"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	contents, streamErr := collectDeltas(t, ch)
	if streamErr != nil {
		t.Fatalf("unexpected stream error: %v", streamErr)
	}

	got := strings.Join(contents, "")
	if got != "Hello world" {
		t.Errorf("got %q, want %q", got, "Hello world")
	}
}

func TestStreamCompletionEmptyContent(t *testing.T) {
	srv := sseServer(
		`data: {"choices":[{"delta":{"content":""}}]}`,
		chunkLine("only"),
		"data: [DONE]",
	)
	defer srv.Close()

	ch, err := StreamCompletion(context.Background(), srv.Client(), CompletionRequest{URL: srv.URL + "/v1/chat/completions", Prompt: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	contents, streamErr := collectDeltas(t, ch)
	if streamErr != nil {
		t.Fatalf("unexpected stream error: %v", streamErr)
	}
	if len(contents) != 1 || contents[0] != "only" {
		t.Errorf("got %v, want [only]", contents)
	}
}

func TestStreamCompletionSkipsNonDataLines(t *testing.T) {
	srv := sseServer(
		": comment",
		"event: ping",
		"",
		chunkLine("data"),
		"data: [DONE]",
	)
	defer srv.Close()

	ch, err := StreamCompletion(context.Background(), srv.Client(), CompletionRequest{URL: srv.URL + "/v1/chat/completions", Prompt: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	contents, streamErr := collectDeltas(t, ch)
	if streamErr != nil {
		t.Fatalf("unexpected stream error: %v", streamErr)
	}
	if len(contents) != 1 || contents[0] != "data" {
		t.Errorf("got %v, want [data]", contents)
	}
}

func TestStreamCompletionNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	_, err := StreamCompletion(context.Background(), srv.Client(), CompletionRequest{URL: srv.URL + "/v1/chat/completions", Prompt: "test"})
	if err == nil {
		t.Fatal("expected error for non-200 response")
	}
	if !strings.Contains(err.Error(), "503") {
		t.Errorf("error should mention status code, got: %v", err)
	}
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		t.Errorf("expected *HTTPError, got %T: %v", err, err)
	} else if httpErr.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected StatusCode=503, got %d", httpErr.StatusCode)
	}
}

func TestStreamCompletionCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := StreamCompletion(ctx, &http.Client{}, CompletionRequest{URL: "http://localhost:0/v1/chat/completions", Prompt: "test"})
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

// TestStreamCompletionWithoutCancelDetachesParent guards the klausctl#204
// fix: callers in non-blocking mode wrap the parent context with
// context.WithoutCancel so that cancelling the parent (e.g. when the MCP
// request handler returns) does not abort the streaming HTTP request and
// thereby cause klaus to kill the freshly-spawned claude process.
func TestStreamCompletionWithoutCancelDetachesParent(t *testing.T) {
	handlerStreaming := make(chan struct{})
	handlerSawCancel := make(chan struct{}, 1)
	releaseHandler := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		flusher.Flush()
		close(handlerStreaming)

		select {
		case <-r.Context().Done():
			handlerSawCancel <- struct{}{}
			return
		case <-releaseHandler:
		}
		_, _ = fmt.Fprintln(w, chunkLine("hello"))
		_, _ = fmt.Fprintln(w, "data: [DONE]")
		flusher.Flush()
	}))
	defer srv.Close()

	parent, cancelParent := context.WithCancel(context.Background())
	streamCtx := context.WithoutCancel(parent)

	ch, err := StreamCompletion(streamCtx, srv.Client(), CompletionRequest{URL: srv.URL + "/v1/chat/completions", Prompt: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Wait until the server has flushed headers and is sitting in its
	// streaming loop, then cancel the parent context. With the
	// WithoutCancel wrapper the server must NOT observe cancellation
	// before we release it.
	<-handlerStreaming
	cancelParent()

	select {
	case <-handlerSawCancel:
		t.Fatal("server saw context cancellation despite WithoutCancel wrapper")
	case <-time.After(100 * time.Millisecond):
	}

	close(releaseHandler)

	contents, streamErr := collectDeltas(t, ch)
	if streamErr != nil {
		t.Fatalf("unexpected stream error: %v", streamErr)
	}
	if len(contents) != 1 || contents[0] != "hello" {
		t.Errorf("got %v, want [hello]", contents)
	}
}

// TestStreamCompletionDirectParentPropagatesCancel demonstrates the
// pre-fix behaviour: passing the request context directly causes the
// server-side handler to observe cancellation when the parent is
// cancelled. This is the regression that klausctl#204 protects against
// for non-blocking callers.
func TestStreamCompletionDirectParentPropagatesCancel(t *testing.T) {
	handlerStreaming := make(chan struct{})
	handlerSawCancel := make(chan struct{}, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		close(handlerStreaming)

		<-r.Context().Done()
		handlerSawCancel <- struct{}{}
	}))
	defer srv.Close()

	parent, cancelParent := context.WithCancel(context.Background())

	_, err := StreamCompletion(parent, srv.Client(), CompletionRequest{URL: srv.URL + "/v1/chat/completions", Prompt: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	<-handlerStreaming
	cancelParent()

	select {
	case <-handlerSawCancel:
	case <-time.After(time.Second):
		t.Fatal("server did not observe cancellation propagated from parent")
	}
}

func TestStreamCompletionMalformedJSON(t *testing.T) {
	srv := sseServer(
		"data: {invalid json}",
		chunkLine("valid"),
		"data: [DONE]",
	)
	defer srv.Close()

	ch, err := StreamCompletion(context.Background(), srv.Client(), CompletionRequest{URL: srv.URL + "/v1/chat/completions", Prompt: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	contents, streamErr := collectDeltas(t, ch)
	if streamErr != nil {
		t.Fatalf("unexpected stream error: %v", streamErr)
	}
	if len(contents) != 1 || contents[0] != "valid" {
		t.Errorf("got %v, want [valid]", contents)
	}
}

func TestStreamCompletionNoChoices(t *testing.T) {
	srv := sseServer(
		`data: {"choices":[]}`,
		chunkLine("ok"),
		"data: [DONE]",
	)
	defer srv.Close()

	ch, err := StreamCompletion(context.Background(), srv.Client(), CompletionRequest{URL: srv.URL + "/v1/chat/completions", Prompt: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	contents, streamErr := collectDeltas(t, ch)
	if streamErr != nil {
		t.Fatalf("unexpected stream error: %v", streamErr)
	}
	if len(contents) != 1 || contents[0] != "ok" {
		t.Errorf("got %v, want [ok]", contents)
	}
}

func TestStreamCompletionChannelClosesWithoutDONE(t *testing.T) {
	srv := sseServer(
		chunkLine("partial"),
	)
	defer srv.Close()

	ch, err := StreamCompletion(context.Background(), srv.Client(), CompletionRequest{URL: srv.URL + "/v1/chat/completions", Prompt: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	contents, streamErr := collectDeltas(t, ch)
	if streamErr != nil {
		t.Fatalf("unexpected stream error: %v", streamErr)
	}
	if len(contents) != 1 || contents[0] != "partial" {
		t.Errorf("got %v, want [partial]", contents)
	}
}

func TestStreamCompletionUnreachable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	_, err := StreamCompletion(ctx, &http.Client{}, CompletionRequest{URL: "http://127.0.0.1:1/v1/chat/completions", Prompt: "test"})
	if err == nil {
		t.Fatal("expected error for unreachable host")
	}
}

func TestWaitForReadySuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/status" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"name":"klaus","version":"dev","agent":{"status":"idle"}}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	err := WaitForReady(context.Background(), srv.Client(), srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWaitForReadyCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := WaitForReady(ctx, &http.Client{Timeout: time.Second}, "http://localhost:0")
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestWaitForReadyTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := WaitForReady(ctx, &http.Client{Timeout: time.Second}, "http://127.0.0.1:1")
	if err == nil {
		t.Fatal("expected error for unreachable endpoint")
	}
}
