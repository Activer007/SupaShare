package safedisplay

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

func TestTerminalEscapingAndBound(t *testing.T) {
	raw := "name\x1b]0;spoof\a\r\n\t\x7f\u009b\u202e\u2028" + string([]byte{0xff})
	got := Text(raw)
	for _, r := range got {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == 0x2028 {
			t.Fatal("terminal control survived")
		}
	}
	for _, part := range []string{"\\u001b", "\\u000d", "\\u007f", "\\u009b", "\\u202e", "\\xff"} {
		if !strings.Contains(got, part) {
			t.Fatalf("missing visible escape %s", part)
		}
	}
	huge := Text(strings.Repeat("界\x1b", 5000))
	if len(huge) > MaxBytes || !utf8.ValidString(huge) || !strings.HasSuffix(huge, "...[truncated]") {
		t.Fatal("output not safely bounded")
	}
	if Text("report-报告.pdf") != "report-报告.pdf" {
		t.Fatal("ordinary display changed")
	}
}

func TestJSONRetainsControlValues(t *testing.T) {
	raw := "name\x1b\n\u009b"
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	var decoded string
	if err := json.Unmarshal(data, &decoded); err != nil || decoded != raw {
		t.Fatal("machine encoding changed value")
	}
}
