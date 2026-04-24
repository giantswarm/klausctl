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
