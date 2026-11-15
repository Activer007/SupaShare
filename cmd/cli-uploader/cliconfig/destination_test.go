package cliconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDestinationExplicitConfirmation(t *testing.T) {
	for _, answer := range []string{"", "y", "no", "yes please", "https://example.com"} {
		if confirmDestination(answer) {
			t.Fatal("implicit destination confirmation")
		}
	}
	for _, answer := range []string{"yes", "YES", " yes "} {
		if !confirmDestination(answer) {
			t.Fatal("explicit confirmation rejected")
		}
	}
}

func TestSaveRejectsDeceptiveDestinationBeforeWrite(t *testing.T) {
	old := configPathLocation
	t.Cleanup(func() { configPathLocation = old })
	configPathLocation = filepath.Join(t.TempDir(), "must-not-exist")
	if err := save("https://trusted.example@evil.example", "synthetic", nil); err == nil {
		t.Fatal("deceptive destination saved")
	}
	if _, err := os.Stat(configPathLocation); !os.IsNotExist(err) {
		t.Fatal("unsafe destination reached storage")
	}
}
