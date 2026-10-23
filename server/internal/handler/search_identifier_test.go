package handler

import (
	"testing"

)

func TestParseQueryNumber(t *testing.T) {
	for _, tc := range []struct {
		query string
		want  int
	}{
		{"MUL-123", 123},
		{"mul-123", 123},
		{"  123  ", 123},
		{"V2-12", 12},
		{"  v2-12  ", 12},
		{"A1-7", 7},
		{"a1b2-42", 42},
		{"12-3", 0},
		{"2FAS-1", 0},
		{"V2-0", 0},
		{"V2--12", 0},
		{"V2-12x", 0},
		{"V_2-12", 0},
		{"MUL-", 0},
		{"search", 0},
		{"0", 0},
		{"", 0},
	} {
		t.Run(tc.query, func(t *testing.T) {
			got, ok := parseQueryNumber(tc.query)
			if got != tc.want || ok != (tc.want > 0) {
				t.Fatalf("parseQueryNumber(%q) = (%d, %t), want (%d, %t)", tc.query, got, ok, tc.want, tc.want > 0)
			}
		})
	}
}

