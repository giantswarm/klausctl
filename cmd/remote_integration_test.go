package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/remote"
)

// gatewayRequest captures the request context we want to assert on.
type gatewayRequest struct {
	Method string
	Path   string
	Auth   string
	Header http.Header
	Body   string
}

// fakeGateway mimics a klaus-gateway /v1/{instance}/chat/completions
// endpoint. It echoes streamed content chunks back to the caller and
// records every incoming request for assertions.
func fakeGateway(t *testing.T, firstStatus int) (*httptest.Server, *[]gatewayRequest) {
	t.Helper()
	var requests []gatewayRequest
	var calls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requests = append(requests, gatewayRequest{
			Method: r.Method,
			Path:   r.URL.Path,
			Auth:   r.Header.Get("Authorization"),
			Header: r.Header.Clone(),
			Body:   string(body),
		})
		n := calls.Add(1)

		if n == 1 && firstStatus != http.StatusOK {
			w.WriteHeader(firstStatus)
			_, _ = fmt.Fprintln(w, `{"error":"unauthorized"}`)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		chunks := []string{"Hello", ", ", "world!"}
		for _, c := range chunks {
			payload := map[string]any{
				"choices": []map[string]any{{"delta": map[string]string{"content": c}}},
			}
			b, _ := json.Marshal(payload)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
			if flusher != nil {
				flusher.Flush()
			}
		}
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	return srv, &requests
}

// fakeTokenEndpoint returns an OAuth token endpoint that issues the given
// access token on every refresh call.
func fakeTokenEndpoint(t *testing.T, newAccess, newRefresh string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  newAccess,
			"token_type":    "Bearer",
			"refresh_token": newRefresh,
			"expires_in":    3600,
		})
	}))
}

func TestStreamRemoteCompletionEndToEnd(t *testing.T) {
	gw, requests := fakeGateway(t, http.StatusOK)
	defer gw.Close()

	paths := &config.Paths{AuthDir: filepath.Join(t.TempDir(), "auth")}
	store := remote.NewAuthStore(paths.AuthDir)
	rec := remote.AuthRecord{
		ServerURL:    gw.URL,
		AccessToken:  "tok-1",
		RefreshToken: "rt-1",
		ExpiresAt:    time.Now().Add(30 * time.Minute),
	}
	if err := store.Put(rec); err != nil {
		t.Fatalf("store.Put: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	target, resStore, resRec, err := resolveRemoteTarget(ctx, gw.URL, "dev", "sess-1", paths)
	if err != nil {
		t.Fatalf("resolveRemoteTarget: %v", err)
	}

	ch, err := streamRemoteCompletion(ctx, gw.Client(), &target, resStore, resRec, "hello")
	if err != nil {
		t.Fatalf("streamRemoteCompletion: %v", err)
	}
	var got strings.Builder
	for d := range ch {
		if d.Err != nil {
			t.Fatalf("delta err: %v", d.Err)
		}
		got.WriteString(d.Content)
	}
	if got.String() != "Hello, world!" {
		t.Errorf("stream content = %q, want %q", got.String(), "Hello, world!")
	}

	if len(*requests) != 1 {
		t.Fatalf("expected 1 gateway request, got %d", len(*requests))
	}
	r := (*requests)[0]
	if r.Method != http.MethodPost {
		t.Errorf("method = %q, want POST", r.Method)
	}
	if r.Path != "/v1/dev/chat/completions" {
		t.Errorf("path = %q, want /v1/dev/chat/completions", r.Path)
	}
	if r.Auth != "Bearer tok-1" {
		t.Errorf("Authorization = %q, want Bearer tok-1", r.Auth)
	}
	if got := r.Header.Get(remote.ChannelHeader); got != remote.ChannelCLI {
		t.Errorf("%s = %q, want %q", remote.ChannelHeader, got, remote.ChannelCLI)
	}
	if got := r.Header.Get(remote.ThreadIDHeader); got != "sess-1" {
		t.Errorf("%s = %q, want sess-1", remote.ThreadIDHeader, got)
	}
	if r.Header.Get(remote.ChannelIDHeader) == "" {
		t.Errorf("%s should be populated", remote.ChannelIDHeader)
	}
	if !strings.Contains(r.Body, `"stream":true`) {
		t.Errorf("body missing stream:true: %s", r.Body)
	}
	if !strings.Contains(r.Body, `"content":"hello"`) {
		t.Errorf("body missing user prompt: %s", r.Body)
	}
}

func TestStreamRemoteCompletionRefreshesOn401(t *testing.T) {
	gw, requests := fakeGateway(t, http.StatusUnauthorized)
	defer gw.Close()

	tokenSrv := fakeTokenEndpoint(t, "new-access-token", "new-refresh-token")
	defer tokenSrv.Close()

	paths := &config.Paths{AuthDir: filepath.Join(t.TempDir(), "auth")}
	store := remote.NewAuthStore(paths.AuthDir)
	rec := remote.AuthRecord{
		ServerURL:     gw.URL,
		AccessToken:   "stale-token",
		RefreshToken:  "rt-1",
		TokenEndpoint: tokenSrv.URL,
		ExpiresAt:     time.Now().Add(30 * time.Minute),
	}
	if err := store.Put(rec); err != nil {
		t.Fatalf("store.Put: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	target, resStore, resRec, err := resolveRemoteTarget(ctx, gw.URL, "dev", "sess-retry", paths)
	if err != nil {
		t.Fatalf("resolveRemoteTarget: %v", err)
	}

	ch, err := streamRemoteCompletion(ctx, gw.Client(), &target, resStore, resRec, "hello")
	if err != nil {
		t.Fatalf("streamRemoteCompletion: %v", err)
	}
	var got strings.Builder
	for d := range ch {
		if d.Err != nil {
			t.Fatalf("delta err: %v", d.Err)
		}
		got.WriteString(d.Content)
	}
	if got.String() != "Hello, world!" {
		t.Errorf("content after refresh = %q, want Hello, world!", got.String())
	}

	if len(*requests) != 2 {
		t.Fatalf("expected 2 gateway requests (401 + retry), got %d", len(*requests))
	}
	if (*requests)[0].Auth != "Bearer stale-token" {
		t.Errorf("first request auth = %q, want stale-token", (*requests)[0].Auth)
	}
	if (*requests)[1].Auth != "Bearer new-access-token" {
		t.Errorf("retry auth = %q, want new-access-token", (*requests)[1].Auth)
	}

	// Refreshed token must have been persisted.
	persisted, err := store.Get(gw.URL)
	if err != nil {
		t.Fatalf("store.Get after refresh: %v", err)
	}
	if persisted == nil || persisted.AccessToken != "new-access-token" || persisted.RefreshToken != "new-refresh-token" {
		t.Errorf("refreshed token not persisted: %+v", persisted)
	}
}

func TestResolveRemoteTargetProactiveRefresh(t *testing.T) {
	tokenSrv := fakeTokenEndpoint(t, "rotated-token", "rotated-refresh")
	defer tokenSrv.Close()

	paths := &config.Paths{AuthDir: filepath.Join(t.TempDir(), "auth")}
	store := remote.NewAuthStore(paths.AuthDir)
	expired := remote.AuthRecord{
		ServerURL:     "https://gw.example.com",
		AccessToken:   "about-to-expire",
		RefreshToken:  "rt-1",
		TokenEndpoint: tokenSrv.URL,
		ExpiresAt:     time.Now().Add(5 * time.Second), // within 60s leeway
	}
	if err := store.Put(expired); err != nil {
		t.Fatalf("store.Put: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	target, _, rec, err := resolveRemoteTarget(ctx, "https://gw.example.com", "dev", "", paths)
	if err != nil {
		t.Fatalf("resolveRemoteTarget: %v", err)
	}
	if target.BearerToken != "rotated-token" {
		t.Errorf("target bearer = %q, want rotated-token", target.BearerToken)
	}
	if rec == nil || rec.AccessToken != "rotated-token" {
		t.Errorf("returned rec not refreshed: %+v", rec)
	}

	persisted, err := store.Get("https://gw.example.com")
	if err != nil {
		t.Fatalf("store.Get: %v", err)
	}
	if persisted == nil || persisted.AccessToken != "rotated-token" {
		t.Errorf("proactive refresh not persisted: %+v", persisted)
	}
}

func TestResolveRemoteTargetNoAuthRecord(t *testing.T) {
	paths := &config.Paths{AuthDir: filepath.Join(t.TempDir(), "auth")}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	target, _, rec, err := resolveRemoteTarget(ctx, "https://gw.example.com", "dev", "", paths)
	if err != nil {
		t.Fatalf("resolveRemoteTarget without stored auth: %v", err)
	}
	if target.BearerToken != "" {
		t.Errorf("expected empty bearer when no auth record, got %q", target.BearerToken)
	}
	if rec != nil {
		t.Errorf("expected nil auth record, got %+v", rec)
	}
}
