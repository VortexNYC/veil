package cli

import "testing"

func TestURLMatchesSuccess(t *testing.T) {
	t.Parallel()
	cases := []struct {
		current, expected string
		want              bool
	}{
		{"https://github.com/", "https://github.com/", true},
		{"https://github.com/dashboard", "https://github.com/", true},
		{"https://github.com/login", "https://github.com/dashboard", false},
		{"https://accounts.google.com/v3/signin/continue", "https://accounts.google.com/", true},
	}
	for _, tc := range cases {
		if got := urlMatchesSuccess(tc.current, tc.expected); got != tc.want {
			t.Fatalf("%q vs %q: got %v want %v", tc.current, tc.expected, got, tc.want)
		}
	}
}
