package oauth

import "golang.org/x/oauth2"

// challengeMethodS256 is the PKCE code challenge method used for all flows.
const challengeMethodS256 = "S256"

// GeneratePKCE creates a new PKCE verifier/challenge pair using the S256
// method. Uses golang.org/x/oauth2's cryptographically secure implementation.
func GeneratePKCE() PKCEChallenge {
	verifier := oauth2.GenerateVerifier()
	return PKCEChallenge{
		Verifier:        verifier,
		Challenge:       oauth2.S256ChallengeFromVerifier(verifier),
		ChallengeMethod: challengeMethodS256,
	}
}
