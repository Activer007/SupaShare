package cliflags

import (
	"github.com/Activer007/SupaShare/cmd/cli-uploader/cliconstants"
	"github.com/Activer007/SupaShare/internal/environment"
	"os"
	"testing"
)

func TestConfigLocationDockerAndExplicit(t *testing.T) {
	oldArgs, oldDocker := os.Args, environment.IsDocker
	t.Cleanup(func() { os.Args = oldArgs; environment.IsDocker = oldDocker })
	environment.IsDocker = "true"
	os.Args = []string{"cli", "login"}
	paths, def := GetConfigLocation()
	if !def || len(paths) != 1 || paths[0] != cliconstants.DockerFolderConfigFile {
		t.Fatalf("Docker path changed: %v", paths)
	}
	os.Args = []string{"cli", "login", "--configuration", "custom.json"}
	paths, def = GetConfigLocation()
	if def || len(paths) != 1 || paths[0] != "custom.json" {
		t.Fatal("explicit path not honored")
	}
}

func TestConfigLocationSkipsConsumedValues(t *testing.T) {
	oldArgs, oldDocker := os.Args, environment.IsDocker
	t.Cleanup(func() { os.Args = oldArgs; environment.IsDocker = oldDocker })
	environment.IsDocker = "true"
	options := []string{"-p", "--password", "--password-file", "-f", "--file", "-e", "--expiry-days", "-d", "--expiry-downloads", "-D", "--directory", "-t", "--tempfolder", "-n", "--name", "-i", "--id", "-o", "--output", "-k", "--output-path"}
	for _, option := range options {
		for _, value := range []string{"-c", "--configuration"} {
			os.Args = []string{"cli", "upload", option, value, "synthetic-not-config"}
			paths, def := GetConfigLocation()
			if !def || len(paths) != 1 || paths[0] != cliconstants.DockerFolderConfigFile {
				t.Fatalf("consumed value interpreted as configuration: %s", option)
			}
			os.Args = append(os.Args, "-c", "explicit.json")
			paths, def = GetConfigLocation()
			if def || len(paths) != 1 || paths[0] != "explicit.json" {
				t.Fatalf("real configuration option lost: %s", option)
			}
		}
	}
	for _, arg := range []string{"--password=-c", "--password-file=--configuration", "--password-prompt"} {
		os.Args = []string{"cli", "upload", arg, "-c", "explicit.json"}
		paths, def := GetConfigLocation()
		if def || paths[0] != "explicit.json" {
			t.Fatal("single-token source consumed next configuration flag")
		}
	}
}
