package oauth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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
			query:      "error=access_denied&error_description=%3Cscript%3Ealert(1)%3C%2Fscript%3E",
			wantStatus: http.StatusBadRequest,
			want:       []string{"<h1>Sign-in failed</h1>", "The sign-in was denied or failed", "<code>access_denied</code>"},
			notWant:    []string{"<script>", "alert(1)"},
		},
		{
			name:       "provider error that is not a registered code",
			query:      "error=your_account_is_locked_call_555",
			wantStatus: http.StatusBadRequest,
			want:       []string{"The sign-in was denied or failed"},
			notWant:    []string{"your_account_is_locked"},
		},
		{
			name:       "state mismatch",
			query:      "code=c&state=forged",
			wantStatus: http.StatusBadRequest,
			want:       []string{"<h1>Sign-in failed</h1>", "The sign-in response could not be verified"},
		},
		{
			name:       "no code",
			query:      "state=" + testState,
			wantStatus: http.StatusBadRequest,
			want:       []string{"<h1>Sign-in failed</h1>", "The sign-in response is incomplete"},
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
// redirect page that loads the callback twice: the second load shows what the
// first one did instead of an empty page.
func TestCallbackServer_DuplicateDeliveryShowsFirstOutcome(t *testing.T) {
	cs := NewCallbackServer(testState)
	handler := cs.handleCallback(testState)

	servePage(handler, "code=c&state="+testState)
	rr := servePage(handler, "code=other&state=other")

	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "<h1>Signed in</h1>") {
		t.Errorf("second load: status %d, body %s", rr.Code, rr.Body.String())
	}
	if result := <-cs.result; result.Code != "c" {
		t.Errorf("result code = %q, want the first callback's", result.Code)
	}
}
