package oauth

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"
)

func TestCallbackServer_SuccessfulCallback(t *testing.T) {
	state := "test-state-123"
	cs := NewCallbackServer(state)

	redirectURI, err := cs.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	go func() {
		url := fmt.Sprintf("%s?code=auth-code-456&state=%s", redirectURI, state)
		resp, err := http.Get(url)
		if err != nil {
			t.Errorf("callback GET: %v", err)
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d: %s", resp.StatusCode, body)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := cs.WaitForCallback(ctx)
	if err != nil {
		t.Fatalf("WaitForCallback: %v", err)
	}
	if result.Code != "auth-code-456" {
		t.Errorf("Code = %q, want auth-code-456", result.Code)
	}
	if result.State != state {
		t.Errorf("State = %q, want %q", result.State, state)
	}
}

func TestCallbackServer_StateMismatch(t *testing.T) {
	cs := NewCallbackServer("expected-state")

	redirectURI, err := cs.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	go func() {
		url := fmt.Sprintf("%s?code=auth-code&state=wrong-state", redirectURI)
		resp, err := http.Get(url)
		if err != nil {
			t.Errorf("callback GET: %v", err)
			return
		}
		resp.Body.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = cs.WaitForCallback(ctx)
	if err == nil {
		t.Fatal("expected error for state mismatch")
	}
}

func TestCallbackServer_OAuthError(t *testing.T) {
	cs := NewCallbackServer("test-state")

	redirectURI, err := cs.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	go func() {
		url := fmt.Sprintf("%s?error=access_denied&error_description=user+denied+access", redirectURI)
		resp, err := http.Get(url)
		if err != nil {
			t.Errorf("callback GET: %v", err)
			return
		}
		resp.Body.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = cs.WaitForCallback(ctx)
	if err == nil {
		t.Fatal("expected error for OAuth error response")
	}
}

func TestCallbackServer_MissingCode(t *testing.T) {
	state := "test-state"
	cs := NewCallbackServer(state)

	redirectURI, err := cs.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	go func() {
		url := fmt.Sprintf("%s?state=%s", redirectURI, state)
		resp, err := http.Get(url)
		if err != nil {
			t.Errorf("callback GET: %v", err)
			return
		}
		resp.Body.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = cs.WaitForCallback(ctx)
	if err == nil {
		t.Fatal("expected error for missing authorization code")
	}
}

func TestCallbackServer_ContextCancelled(t *testing.T) {
	cs := NewCallbackServer("test-state")

	_, err := cs.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err = cs.WaitForCallback(ctx)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestCallbackServer_OnlyProcessesFirstCallback(t *testing.T) {
	state := "test-state"
	cs := NewCallbackServer(state)

	redirectURI, err := cs.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	go func() {
		url := fmt.Sprintf("%s?code=first-code&state=%s", redirectURI, state)
		http.Get(url)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := cs.WaitForCallback(ctx)
	if err != nil {
		t.Fatalf("WaitForCallback: %v", err)
	}
	if result.Code != "first-code" {
		t.Errorf("Code = %q, want first-code (sync.Once should ignore second)", result.Code)
	}
}

func TestCallbackServer_ErrorWithoutDescription(t *testing.T) {
	cs := NewCallbackServer("test-state")

	redirectURI, err := cs.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	go func() {
		url := fmt.Sprintf("%s?error=server_error", redirectURI)
		resp, err := http.Get(url)
		if err != nil {
			t.Errorf("callback GET: %v", err)
			return
		}
		resp.Body.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = cs.WaitForCallback(ctx)
	if err == nil {
		t.Fatal("expected error")
	}
}
