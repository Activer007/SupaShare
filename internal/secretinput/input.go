// Package secretinput resolves explicit secret sources without echoed fallbacks.
package secretinput

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/Activer007/SupaShare/internal/privatefile"
)

const MaxBytes = 64 * 1024

// Source permits exactly one explicit source, or none. Set flags distinguish an
// explicitly empty value from absence (legacy inline empty remains supported). Nonempty values also count as explicit.
type Source struct {
	Inline, File               string
	Prompt, InlineSet, FileSet bool
}

// Resolve reads a secret. No source returns an empty value for optional callers.
// Only recognized fixed labels are shown in prompts. Unknown labels use Secret,
// preventing accidental disclosure of arbitrary caller data. File errors are opaque.
func Resolve(source Source, label string) (string, error) {
	inline := source.InlineSet || source.Inline != ""
	file := source.FileSet || source.File != ""
	count := 0
	for _, set := range []bool{inline, file, source.Prompt} {
		if set {
			count++
		}
	}
	if count > 1 {
		return "", errors.New("select only one secret input source")
	}
	if count == 0 {
		return "", nil
	}
	var value string
	if inline {
		fmt.Fprintln(os.Stderr, "Warning: inline secret arguments may be exposed in process listings and shell history; use a private file or hidden prompt.")
		value = source.Inline
	} else if file {
		data, err := privatefile.ReadLimit(source.File, MaxBytes)
		if err != nil {
			return "", errors.New("cannot read private secret file")
		}
		defer clear(data)
		value = string(data)
		if strings.HasSuffix(value, "\n") {
			value = strings.TrimSuffix(value, "\n")
			value = strings.TrimSuffix(value, "\r")
		}
	} else {
		var err error
		value, err = readHidden(promptLabel(label))
		if err != nil {
			return "", err
		}
	}
	if (value == "" && !inline) || len(value) > MaxBytes || strings.ContainsAny(value, "\x00\r\n") {
		return "", errors.New("secret must be nonempty, single-line and at most 64 KiB")
	}
	return value, nil
}

func promptLabel(label string) string {
	switch label {
	case "Deployment administrator password", "Source database URL", "Destination database URL", "Upload password":
		return label
	default:
		return "Secret"
	}
}
