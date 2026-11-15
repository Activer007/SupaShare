package cliconfig

import (
	"github.com/Activer007/SupaShare/cmd/cli-uploader/cliflags"
	"github.com/Activer007/SupaShare/internal/environment"
	"github.com/Activer007/SupaShare/internal/privatefile"
	"os"
	"path/filepath"
)

func writeCredentials(path string, data []byte) error {
	_, isDefault := cliflags.GetConfigLocation()
	if path != "" && isDefault && !environment.IsDockerInstance() {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return err
		}
	}
	return privatefile.Write(path, data)
}
func readCredentials(path string) ([]byte, error) { return privatefile.Read(path) }

func deleteCredentials(path string) error { return privatefile.Remove(path) }
