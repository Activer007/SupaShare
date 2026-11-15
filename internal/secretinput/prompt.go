package secretinput

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

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
	terminal := term.NewTerminal(struct {
		io.Reader
		io.Writer
	}{os.Stdin, os.Stderr}, "")
	value, err := terminal.ReadPassword(label + " (hidden): ")
	if err != nil {
		// Do not expose backend errors or partial secret input.
		fmt.Fprint(os.Stderr, "\r\n")
		return "", errors.New("could not read hidden input; input cancelled")
	}
	return value, nil
}
