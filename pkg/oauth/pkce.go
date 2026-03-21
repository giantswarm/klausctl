package oauth

import "golang.org/x/oauth2"

// GeneratePKCE creates a new PKCE verifier/challenge pair using the S256
// method. Uses golang.org/x/oauth2's cryptographically secure implementation.
func GeneratePKCE() PKCEChallenge {
	verifier := oauth2.GenerateVerifier()
	return PKCEChallenge{
		Verifier:        verifier,
		Challenge:       oauth2.S256ChallengeFromVerifier(verifier),
		ChallengeMethod: "S256",
	}
}
