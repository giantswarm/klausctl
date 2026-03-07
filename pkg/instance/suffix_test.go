package instance

import (
	"regexp"
	"testing"
)

func TestGenerateSuffix(t *testing.T) {
	suffix, err := GenerateSuffix()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(suffix) != 4 {
		t.Fatalf("expected 4-char suffix, got %q (len %d)", suffix, len(suffix))
	}
	if !regexp.MustCompile(`^[0-9a-f]{4}$`).MatchString(suffix) {
		t.Fatalf("suffix %q is not lowercase hex", suffix)
	}
}

func TestGenerateSuffixUniqueness(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		suffix, err := GenerateSuffix()
		if err != nil {
			t.Fatalf("unexpected error on iteration %d: %v", i, err)
		}
		seen[suffix] = true
	}
	// With 4 hex chars (65536 possibilities), 100 samples should produce
	// at least 90 unique values.
	if len(seen) < 90 {
		t.Fatalf("expected at least 90 unique suffixes out of 100, got %d", len(seen))
	}
}

func TestAppendSuffix(t *testing.T) {
	name, err := AppendSuffix("myproject")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !regexp.MustCompile(`^myproject-[0-9a-f]{4}$`).MatchString(name) {
		t.Fatalf("unexpected name format: %q", name)
	}
}
