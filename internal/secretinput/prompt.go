package secretinput

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"unicode/utf8"

	"golang.org/x/term"
)

// readHidden deliberately has no echoed or redirected-input fallback.
// Unattended commands must explicitly select a private file source.
func readHidden(label string) (string, error) {
	fd := int(os.Stdin.Fd())
	state, err := term.GetState(fd)
	if err != nil {
		return "", errors.New("secret input requires an interactive terminal; run in a terminal (Docker: -it)")
	}
	// Process signals do not unwind defers. Restore before terminating an
	// interrupted prompt, including while the terminal is in raw mode.
	signals := make(chan os.Signal, 1)
	done := make(chan struct{})
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	defer close(done)
	defer term.Restore(fd, state)
	go func() {
		select {
		case <-signals:
			_ = term.Restore(fd, state)
			_, _ = fmt.Fprintln(os.Stderr)
			os.Exit(1)
		case <-done:
		}
	}()
	if _, err := term.MakeRaw(fd); err != nil {
		return "", errors.New("could not enable hidden input; input cancelled")
	}
	if _, err := fmt.Fprint(os.Stderr, label+" (hidden): "); err != nil {
		return "", errors.New("could not display hidden input prompt; input cancelled")
	}
	value, err := readHiddenLine(os.Stdin)
	fmt.Fprint(os.Stderr, "\r\n")
	if err != nil {
		return "", err
	}
	return value, nil
}

// Read raw input directly: term.Terminal silently drops characters after its
// editor limit. Keep a byte-bounded line with basic backspace/clear editing.
func readHiddenLine(input io.Reader) (string, error) {
	reader := bufio.NewReader(input)
	line := make([]byte, 0, MaxBytes)
	defer func() { clear(line) }()
	var invalid error
	for {
		key, err := reader.ReadByte()
		if err != nil {
			return "", errors.New("could not read hidden input; input cancelled")
		}
		switch key {
		case '\r', '\n':
			if invalid != nil {
				return "", invalid
			}
			return string(line), nil
		case 3, 4: // Ctrl-C / Ctrl-D cancel without returning partial input.
			return "", errors.New("could not read hidden input; input cancelled")
		}
		// Consume rejected lines through Enter while echo remains disabled, so
		// queued paste data cannot be reprocessed after terminal restoration.
		if invalid != nil {
			continue
		}
		switch key {
		case '\b', 127:
			_, size := utf8.DecodeLastRune(line)
			clear(line[len(line)-size:])
			line = line[:len(line)-size]
		case 21: // Ctrl-U clears the current line.
			clear(line)
			line = line[:0]
		default:
			if key < 32 {
				invalid = errors.New("unsupported terminal control input; input cancelled")
			} else if len(line) == MaxBytes {
				invalid = errors.New("secret input exceeds 64 KiB; input cancelled")
			} else {
				line = append(line, key)
			}
		}
	}
}
