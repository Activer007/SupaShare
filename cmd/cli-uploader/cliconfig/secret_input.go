package cliconfig

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/term"
)

// readLoginSecret deliberately has no echoed or redirected-input fallback.
// Login is interactive; unattended commands use the existing private config.
func readLoginSecret(prompt string) (string, error) {
	fd := int(os.Stdin.Fd())
	state, err := term.GetState(fd)
	if err != nil {
		return "", errors.New("secret input requires an interactive terminal; run login in a terminal (Docker: -it)")
	}
	// Process signals do not unwind defers. Restore before terminating an
	// interrupted login, including while the terminal is in raw mode.
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
			_, _ = fmt.Fprintln(os.Stdout)
			os.Exit(1)
		case <-done:
		}
	}()
	if _, err := term.MakeRaw(fd); err != nil {
		return "", errors.New("could not enable hidden input; login cancelled")
	}
	terminal := term.NewTerminal(struct {
		io.Reader
		io.Writer
	}{os.Stdin, os.Stdout}, "")
	value, err := terminal.ReadPassword(prompt)
	if err != nil {
		// Do not expose backend errors or partial secret input.
		fmt.Print("\r\n")
		return "", errors.New("could not read hidden input; login cancelled")
	}
	return value, nil
}
