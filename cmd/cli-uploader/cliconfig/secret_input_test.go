package cliconfig

import (
	"io"
	"os"
	"strings"
	"testing"
)

func TestSecretInputRejectsRedirectedStdinWithoutReading(t *testing.T) {
	input, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	const secret = "synthetic-not-a-real-key"
	if _, err := writer.WriteString(secret + "\n"); err != nil {
		t.Fatal(err)
	}
	writer.Close()
	previous := os.Stdin
	os.Stdin = input
	defer func() { os.Stdin = previous }()
	value, err := readLoginSecret("unused prompt")
	if err == nil || value != "" || strings.Contains(err.Error(), secret) {
		t.Fatalf("redirected secret must be rejected without disclosure")
	}
	remaining, err := io.ReadAll(input)
	if err != nil {
		t.Fatal(err)
	}
	if string(remaining) != secret+"\n" {
		t.Fatal("nonterminal secret bytes were consumed")
	}
}

func TestSecretInputRejectsClosedStdin(t *testing.T) {
	input, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	input.Close()
	writer.Close()
	previous := os.Stdin
	os.Stdin = input
	defer func() { os.Stdin = previous }()
	value, err := readLoginSecret("unused prompt")
	if err == nil || value != "" {
		t.Fatal("closed terminal input must fail closed")
	}
}
