package agentclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFetchStatus(t *testing.T) {
	want := StatusResponse{
		Name:    "klaus",
		Version: "dev",
		Agent: AgentInfo{
			Status:       "busy",
			SessionID:    "abc-123",
			MessageCount: 42,
		},
		Mode: "single-shot",
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/status" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(want)
	}))
	defer srv.Close()

	got, err := FetchStatus(context.Background(), srv.Client(), srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Agent.Status != want.Agent.Status {
		t.Errorf("agent status = %q, want %q", got.Agent.Status, want.Agent.Status)
	}
	if got.Agent.SessionID != want.Agent.SessionID {
		t.Errorf("session_id = %q, want %q", got.Agent.SessionID, want.Agent.SessionID)
	}
	if got.Agent.MessageCount != want.Agent.MessageCount {
		t.Errorf("message_count = %d, want %d", got.Agent.MessageCount, want.Agent.MessageCount)
	}
	if got.Mode != want.Mode {
		t.Errorf("mode = %q, want %q", got.Mode, want.Mode)
	}
}

func TestFetchStatusNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := FetchStatus(context.Background(), srv.Client(), srv.URL)
	if err == nil {
		t.Fatal("expected error for non-200 response")
	}
}

func TestFetchStatusUnreachable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	_, err := FetchStatus(ctx, http.DefaultClient, "http://127.0.0.1:1")
	if err == nil {
		t.Fatal("expected error for unreachable host")
	}
}

func TestFetchStatusInvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	_, err := FetchStatus(context.Background(), srv.Client(), srv.URL)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestFetchStatusIdleAgent(t *testing.T) {
	want := StatusResponse{
		Name:    "klaus",
		Version: "1.0.0",
		Agent: AgentInfo{
			Status: "idle",
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(want)
	}))
	defer srv.Close()

	got, err := FetchStatus(context.Background(), srv.Client(), srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Agent.Status != "idle" {
		t.Errorf("agent status = %q, want %q", got.Agent.Status, "idle")
	}
	if got.Agent.MessageCount != 0 {
		t.Errorf("message_count = %d, want 0", got.Agent.MessageCount)
	}
	if got.Agent.SessionID != "" {
		t.Errorf("session_id = %q, want empty", got.Agent.SessionID)
	}
}
