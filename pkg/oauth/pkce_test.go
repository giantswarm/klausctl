package oauth

import (
	"testing"
)

func TestGeneratePKCE(t *testing.T) {
	pkce := GeneratePKCE()

	if pkce.Verifier == "" {
		t.Error("Verifier is empty")
	}
	if pkce.Challenge == "" {
		t.Error("Challenge is empty")
	}
	if pkce.ChallengeMethod != "S256" {
		t.Errorf("ChallengeMethod = %q, want S256", pkce.ChallengeMethod)
	}

	if len(pkce.Verifier) < 43 {
		t.Errorf("Verifier too short: %d chars (RFC 7636 requires 43-128)", len(pkce.Verifier))
	}
}

func TestGeneratePKCEUniqueness(t *testing.T) {
	a := GeneratePKCE()
	b := GeneratePKCE()

	if a.Verifier == b.Verifier {
		t.Error("two consecutive PKCE generations produced the same verifier")
	}
	if a.Challenge == b.Challenge {
		t.Error("two consecutive PKCE generations produced the same challenge")
	}
}
