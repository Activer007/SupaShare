package migration

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/Activer007/SupaShare/internal/environment/flagparser"
)

func TestN07MigrationDiagnosticsDoNotExposeURLs(t *testing.T) {
	oldExit := osExit
	osExit = func(int) {}
	defer func() { osExit = oldExit }()
	marker := "N07-secret-marker"
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = w, w
	defer func() { os.Stdout, os.Stderr = oldOut, oldErr }()
	Do(flagparser.MigrateFlags{Source: "unsupported://user:" + marker + "@host", Destination: "sqlite://ignored"})
	Do(flagparser.MigrateFlags{Source: "redis://user:" + marker + "@%zz", Destination: "sqlite://ignored"})
	w.Close()
	os.Stdout, os.Stderr = oldOut, oldErr
	output, err := io.ReadAll(r)
	r.Close()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(output), marker) || strings.Contains(string(output), "unsupported://") {
		t.Fatal("migration input leaked")
	}
}
