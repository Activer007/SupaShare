package secretinput

import (
	"strings"
	"testing"
)

func TestHiddenLineDoesNotTruncate(t *testing.T) {
	for _, value := range []string{strings.Repeat("a", 5000), strings.Repeat("界", 5000), strings.Repeat("a", MaxBytes)} {
		got, err := readHiddenLine(strings.NewReader(value + "\r"))
		if err != nil || got != value {
			t.Fatal("hidden input truncated or rejected a supported secret", err)
		}
	}
	input := strings.NewReader(strings.Repeat("a", MaxBytes+100) + "\rremaining")
	got, err := readHiddenLine(input)
	if err == nil || got != "" || !strings.Contains(err.Error(), "64 KiB") {
		t.Fatal("oversized input accepted or limit not reported")
	}
}

func TestHiddenLineEditingAndCancellation(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"\b\x7fvalue\r", "value"},
		{"a界\x7fb\n", "ab"},
		{"discard\x15value\r", "value"},
	} {
		got, err := readHiddenLine(strings.NewReader(tc.input))
		if err != nil || got != tc.want {
			t.Fatal("backspace or clear-line editing changed the secret", err)
		}
	}
	for _, value := range []string{"partial\x03", "partial\x04", "partial", "partial\x1b[D\r"} {
		if got, err := readHiddenLine(strings.NewReader(value)); err == nil || got != "" {
			t.Fatal("cancelled or unsupported input returned a partial secret")
		}
	}
}
