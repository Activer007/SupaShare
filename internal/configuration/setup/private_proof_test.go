//go:build test

package setup

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestN06SetupUnsafeParentDoesNotActivateProof(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX mode fixture")
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	oldU, oldP, oldA, oldF := credentialUsername, credentialPassword, credentialAWSTest, credentialFile
	t.Cleanup(func() {
		credentialUsername, credentialPassword, credentialAWSTest, credentialFile = oldU, oldP, oldA, oldF
	})
	t.Setenv("GOKAPI_CONFIG_DIR", dir)
	if err = os.Chmod(dir, 0777); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0700)
	credentialUsername, credentialPassword = "setup", "previous-proof"
	if err = prepareSetupCredentials(); err == nil {
		t.Fatal("accepted unsafe proof directory")
	}
	if credentialPassword != "" || credentialAWSTest != "" || credentialFile != "" {
		t.Fatal("failed proof still active")
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 0 {
		t.Fatal("failed proof left file", err)
	}
}
