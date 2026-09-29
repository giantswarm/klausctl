package server

import (
	"strings"
	"testing"
)

const argName = "name"

func TestUnknownArguments(t *testing.T) {
	declared := map[string]any{argName: nil, "message": nil, "secretEnvVars": nil, "secretFiles": nil, "envVars": nil}
	cases := map[string]struct {
		args map[string]any
		want []string
	}{
		"all declared": {args: map[string]any{argName: "a", "secretEnvVars": map[string]any{}}},
		"misspelled secrets": {
			args: map[string]any{argName: "a", "secrets": []any{}, "secretEnv": map[string]any{}},
			want: []string{`"secretEnv" (did you mean "secretEnvVars"?)`, `"secrets" (did you mean "secretEnvVars" or "secretFiles"?)`, "nothing was done", "klaus_run takes: envVars, message, name, secretEnvVars, secretFiles"},
		},
		"typo":      {args: map[string]any{"nmae": "a"}, want: []string{`"nmae" (did you mean "name"?)`}},
		"no nearby": {args: map[string]any{"xyz": 1}, want: []string{`unknown argument(s) "xyz"; nothing`}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := unknownArguments("klaus_run", declared, tc.args)
			if tc.want == nil {
				if got != "" {
					t.Fatalf("want no error, got %q", got)
				}
				return
			}
			for _, w := range tc.want {
				if !strings.Contains(got, w) {
					t.Errorf("%q does not contain %q", got, w)
				}
			}
		})
	}
}
