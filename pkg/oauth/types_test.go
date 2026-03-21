package oauth

import (
	"testing"
	"time"
)

func TestStoredToken_IsExpired_NoExpiry(t *testing.T) {
	st := &StoredToken{
		Token:     Token{AccessToken: "test"},
		CreatedAt: time.Now().Add(-24 * time.Hour),
	}
	if st.IsExpired() {
		t.Error("token with no ExpiresIn should not be expired")
	}
}

func TestStoredToken_IsExpired_NegativeExpiresIn(t *testing.T) {
	st := &StoredToken{
		Token:     Token{AccessToken: "test", ExpiresIn: -1},
		CreatedAt: time.Now(),
	}
	if st.IsExpired() {
		t.Error("token with negative ExpiresIn should not be expired")
	}
}

func TestStoredToken_IsExpired_Fresh(t *testing.T) {
	st := &StoredToken{
		Token:     Token{AccessToken: "test", ExpiresIn: 3600},
		CreatedAt: time.Now(),
	}
	if st.IsExpired() {
		t.Error("freshly created token with 1h expiry should not be expired")
	}
}

func TestStoredToken_IsExpired_Expired(t *testing.T) {
	st := &StoredToken{
		Token:     Token{AccessToken: "test", ExpiresIn: 3600},
		CreatedAt: time.Now().Add(-2 * time.Hour),
	}
	if !st.IsExpired() {
		t.Error("token created 2h ago with 1h expiry should be expired")
	}
}

func TestStoredToken_IsExpired_Boundary(t *testing.T) {
	st := &StoredToken{
		Token:     Token{AccessToken: "test", ExpiresIn: 60},
		CreatedAt: time.Now().Add(-61 * time.Second),
	}
	if !st.IsExpired() {
		t.Error("token at boundary should be expired")
	}
}
