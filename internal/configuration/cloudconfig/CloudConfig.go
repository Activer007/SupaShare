package cloudconfig

import (
	"errors"
	"fmt"
	"os"

	"github.com/Activer007/SupaShare/internal/environment"
	"github.com/Activer007/SupaShare/internal/helper"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/privatefile"
	"gopkg.in/yaml.v3"
)

// CloudConfig contains all configuration values / credentials for cloud storage
type CloudConfig struct {
	Aws models.AwsConfig `yaml:"aws"`
}

// Load loads cloud storage configuration / credentials from env variables or data/cloudconfig.yml
func Load() (CloudConfig, bool) {
	env := environment.New()
	if env.IsAwsProvided() {
		return loadFromEnv(&env), true
	}
	path := env.ConfigDir + "/cloudconfig.yml"
	exists, err := privatefile.Exists(path)
	helper.Check(err)
	if exists {
		return loadFromFile(path)
	}
	return CloudConfig{}, false
}

// Write saves the cloudconfig file to the set config path
func Write(config CloudConfig) error {
	_, configDir, _, awsConfigPath := environment.GetConfigPaths()
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return err
	}
	data, err := yaml.Marshal(config)
	if err != nil {
		return err
	}
	return privatefile.Write(awsConfigPath, data)
}

// Delete removes the cloud config file from the set config path
func Delete() error {
	_, _, _, awsConfigPath := environment.GetConfigPaths()
	return privatefile.Remove(awsConfigPath)
}

func loadFromEnv(env *environment.Environment) CloudConfig {
	return CloudConfig{Aws: models.AwsConfig{
		Bucket:        env.AwsBucket,
		Region:        env.AwsRegion,
		Endpoint:      env.AwsEndpoint,
		KeyId:         env.AwsKeyId,
		KeySecret:     env.AwsKeySecret,
		ProxyDownload: env.AwsProxyDownload,
	}}
}

func loadFromFile(path string) (CloudConfig, bool) {
	var result CloudConfig
	file, err := privatefile.Read(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return CloudConfig{}, false
		}
		// Unsafe/unreadable secret storage must not silently switch backends.
		helper.Check(fmt.Errorf("refusing unsafe or unreadable cloud configuration: %w", err))
		return CloudConfig{}, false
	}
	err = yaml.Unmarshal(file, &result)
	if err != nil {
		fmt.Println("Warning: cloudconfig.yml contains invalid yaml!")
		return CloudConfig{}, false
	}
	return result, true
}
