// Package safedisplay renders untrusted strings for human terminals. Machine
// JSON output should instead retain original values through encoding/json.
package safedisplay

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const MaxBytes = 2048

// Text visibly escapes controls, format characters and malformed UTF-8. The
// output is bounded to MaxBytes bytes, including its explicit truncation mark.
func Text(input string) string {
	const truncated = "...[truncated]"
	var result strings.Builder
	for len(input) > 0 {
		r, n := utf8.DecodeRuneInString(input)
		var token string
		if r == utf8.RuneError && n == 1 {
			token = fmt.Sprintf("\\x%02x", input[0])
		} else if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == 0x2028 || r == 0x2029 {
			token = fmt.Sprintf("\\u%04x", r)
		} else {
			token = input[:n]
		}
		if result.Len()+len(token) > MaxBytes-len(truncated) {
			result.WriteString(truncated)
			break
		}
		result.WriteString(token)
		input = input[n:]
	}
	return result.String()
}
