package oauth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const signInFailedHeading = "<h1>Sign-in failed</h1>"

func servePage(handler http.HandlerFunc, query string) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	handler(rr, httptest.NewRequest(http.MethodGet, callbackPath+"?"+query, nil))
	return rr
}

func TestCallbackServer_Pages(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		wantStatus int
		want       []string
		notWant    []string
	}{
		{
			name:       "signed in",
			query:      "code=c&state=" + testState,
			wantStatus: http.StatusOK,
			want:       []string{"<h1>Signed in</h1>", "klausctl is now signed in", "return to where you started the sign-in"},
		},
		{
			name:       "provider refuses",
			query:      "error=access_denied&error_description=%3Cscript%3Ealert(1)%3C%2Fscript%3E&state=" + testState,
			wantStatus: http.StatusBadRequest,
			want:       []string{signInFailedHeading, "The sign-in was denied or failed", "<code>access_denied</code>"},
			notWant:    []string{"<script>", "alert(1)"},
		},
		{
			name:       "provider error that is not a registered code",
			query:      "error=your_account_is_locked_call_555&state=" + testState,
			wantStatus: http.StatusBadRequest,
			want:       []string{"The sign-in was denied or failed"},
			notWant:    []string{"your_account_is_locked"},
		},
		{
			name:       "error without this sign-in's state",
			query:      "error=access_denied",
			wantStatus: http.StatusBadRequest,
			want:       []string{signInFailedHeading, "The sign-in response could not be verified"},
			notWant:    []string{"access_denied"},
		},
		{
			name:       "state mismatch",
			query:      "code=c&state=forged",
			wantStatus: http.StatusBadRequest,
			want:       []string{signInFailedHeading, "The sign-in response could not be verified"},
		},
		{
			name:       "no code",
			query:      "state=" + testState,
			wantStatus: http.StatusBadRequest,
			want:       []string{signInFailedHeading, "The sign-in response is incomplete"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cs := NewCallbackServer(testState)

			rr := servePage(cs.handleCallback(testState), tc.query)

			if rr.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d", rr.Code, tc.wantStatus)
			}
			if !strings.Contains(rr.Header().Get("Content-Security-Policy"), "default-src 'none'") {
				t.Errorf("expected a strict Content-Security-Policy, got %q", rr.Header().Get("Content-Security-Policy"))
			}
			body := rr.Body.String()
			for _, want := range tc.want {
				if !strings.Contains(body, want) {
					t.Errorf("body does not contain %q: %s", want, body)
				}
			}
			for _, notWant := range tc.notWant {
				if strings.Contains(body, notWant) {
					t.Errorf("body contains %q", notWant)
				}
			}
		})
	}
}

// TestCallbackServer_DuplicateDeliveryShowsFirstOutcome covers a browser or
// redirect page that loads the callback twice before the server stops: the
// second load shows what the first one did instead of an empty page, and the
// CLI gets one result.
func TestCallbackServer_DuplicateDeliveryShowsFirstOutcome(t *testing.T) {
	cs := NewCallbackServer(testState)
	handler := cs.handleCallback(testState)

	servePage(handler, "code=c&state="+testState)
	rr := servePage(handler, "code=c&state="+testState)

	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "<h1>Signed in</h1>") {
		t.Errorf("second load: status %d, body %s", rr.Code, rr.Body.String())
	}
	if result := <-cs.result; result.Code != "c" {
		t.Errorf("result code = %q, want the first callback's", result.Code)
	}
}

// TestCallbackServer_ForgedCallbackLeavesSignInWaiting covers a page that
// sends the browser to the local callback during a sign-in: neither a forged
// error nor a forged code reaches the CLI.
func TestCallbackServer_ForgedCallbackLeavesSignInWaiting(t *testing.T) {
	cs := NewCallbackServer(testState)
	handler := cs.handleCallback(testState)

	servePage(handler, "error=access_denied")
	servePage(handler, "code=forged&state=other")
	rr := servePage(handler, "code=real&state="+testState)

	if rr.Code != http.StatusOK {
		t.Errorf("real callback: status %d", rr.Code)
	}
	if result := <-cs.result; result.Code != "real" {
		t.Errorf("result = %+v, want the real callback's code", result)
	}
}

// TestCallbackServer_ErrorDescriptionForTheTerminal covers the description a
// refused sign-in reports in the terminal: control characters, which could
// move the cursor or rewrite the terminal, are dropped.
func TestCallbackServer_ErrorDescriptionForTheTerminal(t *testing.T) {
	cs := NewCallbackServer(testState)

	servePage(cs.handleCallback(testState), "error=access_denied&error_description=%1B%5B2Jdenied%07&state="+testState)

	if result := <-cs.result; result.Error != "[2Jdenied" {
		t.Errorf("Error = %q, want the description without control characters", result.Error)
	}
}

// TestCallbackServer_PageAfterServerFailure covers a request that reaches the
// handler after the server failed: it gets an error page, not a panic.
func TestCallbackServer_PageAfterServerFailure(t *testing.T) {
	cs := NewCallbackServer(testState)
	cs.serverFailed(errors.New("listener closed"))

	rr := servePage(cs.handleCallback(testState), "code=c&state="+testState)

	if rr.Code != http.StatusInternalServerError || !strings.Contains(rr.Body.String(), signInFailedHeading) {
		t.Errorf("status %d, body %s", rr.Code, rr.Body.String())
	}
}
