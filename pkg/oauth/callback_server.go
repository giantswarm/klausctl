package oauth

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"

	"github.com/giantswarm/pagekit"
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

	// status and page answer the first callback, set inside once, and every
	// later load of the callback.
	status int
	page   pagekit.Page
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
		// Only the first callback counts. Browsers and redirect pages may
		// load the callback twice; a later load shows the same outcome.
		cs.once.Do(func() {
			q := r.URL.Query()

			if errParam := q.Get("error"); errParam != "" {
				desc := q.Get("error_description")
				if desc == "" {
					desc = errParam
				}
				cs.result <- CallbackResult{Error: desc}
				cs.status, cs.page = http.StatusBadRequest, failedPage("The sign-in was denied or failed", errParam)
				return
			}

			state := q.Get("state")
			if state != expectedState {
				cs.result <- CallbackResult{Error: "state mismatch"}
				cs.status, cs.page = http.StatusBadRequest, failedPage("The sign-in response could not be verified", "")
				return
			}

			code := q.Get("code")
			if code == "" {
				cs.result <- CallbackResult{Error: "missing authorization code"}
				cs.status, cs.page = http.StatusBadRequest, failedPage("The sign-in response is incomplete", "")
				return
			}

			cs.result <- CallbackResult{Code: code, State: state}
			cs.status, cs.page = http.StatusOK, signedInPage
		})
		pagekit.Render(w, cs.status, cs.page)
	}
}

var signedInPage = pagekit.Page{
	Title:   "Signed in with klausctl",
	Heading: "Signed in",
	Summary: "klausctl is now signed in",
	Message: "You can close this tab and return to where you started the sign-in.",
}

// failedPage returns the page for a failed sign-in. errorCode is shown only
// when it is a registered OAuth error code: anyone can send the browser to
// the local callback with an error of their choosing.
func failedPage(summary, errorCode string) pagekit.Page {
	return pagekit.Page{
		Title:   "Sign-in with klausctl failed",
		Heading: "Sign-in failed",
		Summary: summary,
		Message: "You can close this tab and start the sign-in again from where you started it.",
		Detail:  pagekit.OAuthErrorCode(errorCode),
	}
}
