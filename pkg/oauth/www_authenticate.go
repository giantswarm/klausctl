package oauth

import (
	"strings"
)

// ParseWWWAuthenticate extracts realm and resource_metadata from a
// WWW-Authenticate: Bearer response header value. It handles both
// quoted and unquoted parameter values, and is tolerant of extra
// whitespace.
//
// Examples of supported formats:
//
//	Bearer realm="https://dex.example.com"
//	Bearer realm="https://dex.example.com", resource_metadata="https://mcp.example.com/.well-known/oauth-protected-resource"
//	Bearer realm="https://dex.example.com",resource_metadata="https://mcp.example.com/.well-known/oauth-protected-resource"
func ParseWWWAuthenticate(header string) *AuthChallenge {
	header = strings.TrimSpace(header)

	if !strings.HasPrefix(strings.ToLower(header), "bearer ") {
		return nil
	}
	params := header[len("bearer "):]

	challenge := &AuthChallenge{}
	for _, pair := range splitParams(params) {
		key, value := parseKeyValue(pair)
		switch strings.ToLower(key) {
		case "realm":
			challenge.Realm = value
		case "resource_metadata":
			challenge.ResourceMetadata = value
		}
	}

	if challenge.Realm == "" && challenge.ResourceMetadata == "" {
		return nil
	}
	return challenge
}

// splitParams splits comma-separated key=value pairs, respecting quoted values.
func splitParams(s string) []string {
	var parts []string
	var current strings.Builder
	inQuote := false
	escaped := false

	for _, r := range s {
		if escaped {
			current.WriteRune(r)
			escaped = false
			continue
		}
		switch {
		case r == '\\':
			escaped = true
			current.WriteRune(r)
		case r == '"':
			inQuote = !inQuote
			current.WriteRune(r)
		case r == ',' && !inQuote:
			parts = append(parts, strings.TrimSpace(current.String()))
			current.Reset()
		default:
			current.WriteRune(r)
		}
	}
	if current.Len() > 0 {
		parts = append(parts, strings.TrimSpace(current.String()))
	}
	return parts
}

// parseKeyValue splits "key=value" or "key=\"value\"" into key and unquoted value.
func parseKeyValue(s string) (string, string) {
	s = strings.TrimSpace(s)
	idx := strings.Index(s, "=")
	if idx < 0 {
		return s, ""
	}
	key := strings.TrimSpace(s[:idx])
	value := strings.TrimSpace(s[idx+1:])
	value = strings.Trim(value, "\"")
	return key, value
}
