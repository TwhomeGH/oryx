package main

import "testing"

// The key is the identity: it must be derivable from the label an operator sees, and stable.
func TestForwardSlug(t *testing.T) {
	cases := map[string]string{
		"RStream":       "rstream",
		"YouTube":       "youtube",
		"my tv channel": "my-tv-channel",
		"直播測試":          "custom",
		"  spaced  ":    "spaced",
		"A/B+C":         "a-b-c",
	}
	for in, want := range cases {
		if got := forwardSlug(in, "custom"); got != want {
			t.Fatalf("forwardSlug(%q) = %q, want %q", in, got, want)
		}
	}
	// Length is capped so the key stays readable in logs and URLs.
	long := "abcdefghijklmnopqrstuvwxyz0123456789"
	if got := forwardSlug(long, "custom"); len(got) > 24 {
		t.Fatalf("slug not capped: %d chars", len(got))
	}
}
