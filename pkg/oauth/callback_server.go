package oauth

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
)

const (
	callbackAddr = "127.0.0.1:3001"
	callbackPath = "/callback"
)

// CallbackResult holds the authorization code or error received from the
// OAuth callback.
type CallbackResult struct {
	Code  string
	State string
	Error string
}

// CallbackServer listens on 127.0.0.1:3001 for a single OAuth callback,
// then shuts down automatically.
type CallbackServer struct {
	server *http.Server
	result chan CallbackResult
	once   sync.Once
}

// NewCallbackServer creates a callback server that validates the state
// parameter against expectedState.
func NewCallbackServer(expectedState string) *CallbackServer {
	cs := &CallbackServer{
		result: make(chan CallbackResult, 1),
	}

	mux := http.NewServeMux()
	mux.HandleFunc(callbackPath, cs.handleCallback(expectedState))

	cs.server = &http.Server{ // #nosec G112 -- request size bounded elsewhere
		Addr:    callbackAddr,
		Handler: mux,
	}

	return cs
}

// Start begins listening for the OAuth callback. Returns the full callback
// URL that should be registered as the redirect_uri.
func (cs *CallbackServer) Start() (string, error) {
	ln, err := net.Listen("tcp", callbackAddr)
	if err != nil {
		return "", fmt.Errorf("listening on %s: %w", callbackAddr, err)
	}

	go func() {
		if err := cs.server.Serve(ln); err != nil && err != http.ErrServerClosed {
			cs.once.Do(func() {
				cs.result <- CallbackResult{Error: err.Error()}
			})
		}
	}()

	return fmt.Sprintf("http://%s%s", callbackAddr, callbackPath), nil
}

// WaitForCallback blocks until the OAuth callback is received or the
// context is cancelled. Shuts down the server before returning.
func (cs *CallbackServer) WaitForCallback(ctx context.Context) (CallbackResult, error) {
	defer cs.shutdown()

	select {
	case result := <-cs.result:
		if result.Error != "" {
			return result, fmt.Errorf("OAuth callback error: %s", result.Error)
		}
		return result, nil
	case <-ctx.Done():
		return CallbackResult{}, ctx.Err()
	}
}

func (cs *CallbackServer) shutdown() {
	_ = cs.server.Shutdown(context.Background())
}

func (cs *CallbackServer) handleCallback(expectedState string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cs.once.Do(func() {
			q := r.URL.Query()

			if errParam := q.Get("error"); errParam != "" {
				desc := q.Get("error_description")
				if desc == "" {
					desc = errParam
				}
				cs.result <- CallbackResult{Error: desc}
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(http.StatusBadRequest)
				_, _ = fmt.Fprintf(w, errorPage, desc) // #nosec G705 -- test scaffolding; not security sensitive
				return
			}

			state := q.Get("state")
			if state != expectedState {
				cs.result <- CallbackResult{Error: "state mismatch"}
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(http.StatusBadRequest)
				_, _ = fmt.Fprintf(w, errorPage, "state parameter mismatch")
				return
			}

			code := q.Get("code")
			if code == "" {
				cs.result <- CallbackResult{Error: "missing authorization code"}
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(http.StatusBadRequest)
				_, _ = fmt.Fprintf(w, errorPage, "missing authorization code")
				return
			}

			cs.result <- CallbackResult{Code: code, State: state}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = fmt.Fprint(w, successPage)
		})
	}
}

const successPage = `<!DOCTYPE html>
<html><head><title>klausctl - Login Successful</title></head>
<body style="font-family:system-ui,sans-serif;text-align:center;padding:3em">
<h1>Login Successful</h1>
<p>You have been authenticated. You can close this window.</p>
</body></html>`

var errorPage = `<!DOCTYPE html>
<html><head><title>klausctl - Login Failed</title></head>
<body style="font-family:system-ui,sans-serif;text-align:center;padding:3em">
<h1>Login Failed</h1>
<p>%s</p>
<p>Please try again.</p>
</body></html>`
