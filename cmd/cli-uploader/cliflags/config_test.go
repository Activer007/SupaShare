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
