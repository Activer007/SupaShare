package secretinput

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Activer007/SupaShare/internal/privatefile"
)

func TestSourceRules(t *testing.T) {
	for _, source := range []Source{{InlineSet: true, FileSet: true}, {FileSet: true, Prompt: true}, {Inline: "synthetic", Prompt: true}} {
		value, err := Resolve(source, "unused")
		if err == nil || value != "" {
			t.Fatal("conflicting sources accepted")
		}
	}
	if value, err := Resolve(Source{}, "unused"); err != nil || value != "" {
		t.Fatal("optional absence changed")
	}
	if value, err := Resolve(Source{InlineSet: true}, "unused"); err != nil || value != "" {
		t.Fatal("legacy empty changed")
	}
	for _, value := range []string{"x\ny", "x\ry", "x\x00y", strings.Repeat("x", MaxBytes+1)} {
		got, err := Resolve(Source{Inline: value}, "unused")
		if err == nil || got != "" || strings.Contains(err.Error(), value) {
			t.Fatal("invalid inline or diagnostic leak")
		}
	}
}

func TestPrivateFileSources(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("native private ACL fixtures required")
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "synthetic-secret")
	for _, tc := range []struct {
		input, want string
		valid       bool
	}{
		{"  value  ", "  value  ", true}, {"value\n", "value", true}, {"value\r\n", "value", true},
		{"", "", false}, {"\n", "", false}, {"a\n\n", "", false}, {"a\rb", "", false}, {"a\x00b", "", false},
		{strings.Repeat("x", MaxBytes), strings.Repeat("x", MaxBytes), true}, {strings.Repeat("x", MaxBytes+1), "", false},
	} {
		if err := privatefile.Write(path, []byte(tc.input)); err != nil {
			t.Fatal(err)
		}
		got, err := Resolve(Source{File: path}, "unused")
		if (err == nil) != tc.valid || got != tc.want {
			t.Fatal("file source validation mismatch")
		}
		if err != nil && strings.Contains(err.Error(), path) {
			t.Fatal("path disclosed")
		}
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(Source{File: path}, "unused"); err == nil {
		t.Fatal("unsafe file accepted")
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(Source{File: link}, "unused"); err == nil {
		t.Fatal("symlink accepted")
	}
}

func TestPromptRejectsPipeWithoutConsumingOrDisclosing(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	secret := []byte("synthetic-not-real\n")
	writer.Write(secret)
	writer.Close()
	old := os.Stdin
	os.Stdin = reader
	defer func() { os.Stdin = old }()
	value, err := Resolve(Source{Prompt: true}, "unused")
	if err == nil || value != "" || strings.Contains(err.Error(), "synthetic") {
		t.Fatal("nonterminal input accepted/disclosed")
	}
	remaining, _ := io.ReadAll(reader)
	if !bytes.Equal(remaining, secret) {
		t.Fatal("pipe consumed")
	}
}

func TestPromptLabelWhitelist(t *testing.T) {
	for _, label := range []string{"Deployment administrator password", "Source database URL", "Destination database URL", "Upload password"} {
		if promptLabel(label) != label {
			t.Fatal("recognized label lost")
		}
	}
	if promptLabel("secret-path-and-token") != "Secret" {
		t.Fatal("unknown label leaked")
	}
}
