package shaper

import (
	"errors"
	"strings"
	"testing"
)

func TestExtractJSON(t *testing.T) {
	cases := map[string]struct {
		in   string
		want string
	}{
		"fenced json":     {"pre\n```json\n{\"a\":1}\n```\npost", `{"a":1}`},
		"bare fence":      {"```\n{\"a\":1}\n```", `{"a":1}`},
		"no fence":        {"here it is: {\"a\":1} done", `{"a":1}`},
		"nested braces":   {"```json\n{\"a\":{\"b\":2}}\n```", `{"a":{"b":2}}`},
		"brace in string": {`{"name":"a{b}c"}`, `{"name":"a{b}c"}`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := extractJSON(c.in)
			if err != nil {
				t.Fatalf("extractJSON: %v", err)
			}
			if strings.TrimSpace(string(got)) != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestExtractJSON_NoObject(t *testing.T) {
	if _, err := extractJSON("no json at all"); err == nil {
		t.Error("expected error when no JSON object present")
	}
}

func TestUserPrompt(t *testing.T) {
	if got := userPrompt("describe f", nil); got != "describe f" {
		t.Errorf("first attempt should be the bare description, got %q", got)
	}
	got := userPrompt("describe f", errors.New("missing name"))
	if !strings.Contains(got, "describe f") || !strings.Contains(got, "missing name") {
		t.Errorf("retry prompt missing description or error: %q", got)
	}
}
