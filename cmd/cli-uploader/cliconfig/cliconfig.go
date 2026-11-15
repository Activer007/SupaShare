package cliconfig

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/Activer007/SupaShare/cmd/cli-uploader/cliapi"
	"github.com/Activer007/SupaShare/cmd/cli-uploader/cliconstants"
	"github.com/Activer007/SupaShare/cmd/cli-uploader/cliflags"
	"github.com/Activer007/SupaShare/internal/helper"
	"github.com/Activer007/SupaShare/internal/safedisplay"
	"github.com/Activer007/SupaShare/internal/serviceurl"
	"golang.org/x/term"
)

type configFile struct {
	Url    string `json:"Url"`
	Apikey string `json:"Apikey"`
	E2ekey []byte `json:"E2Ekey"`
}

var configPathLocation string

// CreateLogin creates a login for the CLI.
// It will ask the user for the URL and API key.
// It will then test the connection and download the configuration.
// If the configuration is valid, the login information will be saved.
func CreateLogin() {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Println("ERROR: Login requires an interactive terminal")
		os.Exit(1)
	}
	fmt.Print("SupaShare URL: ")
	rawURL := helper.ReadLine()
	url, err := serviceurl.Normalize(rawURL)
	if err != nil {
		fmt.Println("ERROR: Invalid service URL; credentials, query, fragment and ambiguous paths are not allowed.")
		os.Exit(1)
	}
	// Keep the historical /admin login convenience, but disclose the exact base
	// URL and require confirmation before asking for any secret.
	url = strings.TrimSuffix(url, "/admin")
	fmt.Println("Service destination: " + safedisplay.Text(url))
	if strings.HasPrefix(url, "http://") {
		fmt.Println("WARNING: HTTP sends your API key and data without transport encryption.")
	}
	fmt.Print("Send credentials to this destination? Type yes to continue: ")
	if !confirmDestination(helper.ReadLine()) {
		fmt.Println("Login cancelled; no credentials sent.")
		return
	}
	apikey, err := readLoginSecret("API key (input hidden): ")
	if err != nil {
		fmt.Println("ERROR:", err)
		os.Exit(1)
	}
	if len(apikey) < 3 {
		fmt.Println("ERROR: Invalid API key")
		os.Exit(1)
	}
	fmt.Println("")
	fmt.Print("Testing connection...")
	if err := cliapi.Init(url, apikey, []byte{}); err != nil {
		fmt.Println("ERROR: Invalid service destination")
		os.Exit(1)
	}
	vstr, vint, err := cliapi.GetVersion()
	if err != nil {
		fmt.Println()
		if errors.Is(err, cliapi.ErrUnauthorised) {
			fmt.Println("ERROR: Unauthorised API key")
			os.Exit(1)
		}
		if errors.Is(err, cliapi.ErrNotFound) {
			fmt.Println("ERROR: API not found")
			fmt.Println("The provided URL does not respond to API calls as expected. You most likely entered an incorrect URL.")
			os.Exit(1)
		}
		if errors.Is(err, cliapi.ErrInvalidRequest) {
			fmt.Println("ERROR: API does not support SupaShare CLI")
			fmt.Println("This is most likely caused by an old SupaShare version. Please make sure that your SupaShare instance is running v2.1.0 or newer.")
			os.Exit(1)
		}
		fmt.Println(safedisplay.Text(err.Error()))
		os.Exit(1)
	}

	if vint < cliconstants.MinGokapiVersionInt {
		fmt.Println("\nERROR: SupaShare version must be at least " + cliconstants.MinGokapiVersionStr)
		fmt.Println("Your version is " + safedisplay.Text(vstr))
		os.Exit(1)
	}
	fmt.Print("OK\nDownloading configuration...")

	_, _, isE2E, err := cliapi.GetConfig()
	if err != nil {
		fmt.Println("FAIL")
		if errors.Is(err, cliapi.ErrUnauthorised) {
			fmt.Println("ERROR: API key does not have the permission to upload new files.")
		} else {
			fmt.Println(safedisplay.Text(err.Error()))
		}
		os.Exit(1)
	}
	fmt.Println("OK")
	var e2ekey []byte
	if isE2E {
		e2ekeyString, readErr := readLoginSecret("End-to-end encryption key (input hidden): ")
		if readErr != nil {
			fmt.Println("ERROR:", readErr)
			os.Exit(1)
		}
		e2ekey, err = base64.StdEncoding.DecodeString(e2ekeyString)
		if err != nil {
			fmt.Println("ERROR: Invalid end-to-end encryption key")
			os.Exit(1)
		}
		if err := cliapi.Init(url, apikey, e2ekey); err != nil {
			fmt.Println("ERROR: Invalid service destination")
			os.Exit(1)
		}
		_, err = cliapi.GetE2eInfo(true)
		if err != nil {
			if errors.Is(err, cliapi.ErrE2eKeyIncorrect) {
				fmt.Println("ERROR: Incorrect end-to-end encryption key")
			} else if errors.Is(err, cliapi.ErrE2eNotSetUp) {
				fmt.Println("ERROR: End-to-end encryption has not been set up on the server yet.")
				fmt.Println("Please complete the E2E setup in the SupaShare web interface first, then try again.")
			} else {
				fmt.Println(safedisplay.Text(err.Error()))
			}
			os.Exit(1)
		}
	}

	err = save(url, apikey, e2ekey)
	if err != nil {
		fmt.Println("ERROR: Could not save login information")
		fmt.Println(safedisplay.Text(err.Error()))
		os.Exit(1)
	}
	fmt.Println("Login successful")
	fmt.Print("Credentials saved to: ")
	fmt.Println(safedisplay.Text(getConfigPath()))
}

// getConfigPath selects only the private default or an explicitly requested path.
// Legacy current-directory and global files are deliberately not auto-discovered.
func getConfigPath() string {
	if configPathLocation == "" {
		paths, _ := cliflags.GetConfigLocation()
		configPathLocation = paths[0]
	}
	return configPathLocation
}

func save(url, apikey string, e2ekey []byte) error {
	normalized, err := serviceurl.Normalize(url)
	if err != nil {
		return err
	}
	url = normalized
	configData := configFile{
		Url:    url,
		Apikey: apikey,
		E2ekey: e2ekey,
	}

	jsonData, err := json.Marshal(configData)
	if err != nil {
		return err
	}

	return writeCredentials(getConfigPath(), jsonData)
}

// Load initialises the configuration by reading login information from a file and setting up CLI API parameters.
// Verifies the existence of the configuration file and validates its integrity, terminating on errors.
func Load() {
	data, err := readCredentials(getConfigPath())
	if os.IsNotExist(err) {
		fmt.Println("ERROR: No login information found")
		fmt.Println("Please run 'supashare-cli login' to create a login")
		os.Exit(1)
	}
	if err != nil {
		fmt.Println("ERROR: Could not read private login information")
		os.Exit(1)
	}

	var config configFile
	err = json.Unmarshal(data, &config)
	if err != nil {
		fmt.Println("ERROR: Could not read login information")
		os.Exit(1)
	}

	normalized, err := serviceurl.Normalize(config.Url)
	if err != nil {
		fmt.Println("ERROR: Stored service URL is unsafe; log in again with a verified destination")
		os.Exit(1)
	}
	if err := cliapi.Init(normalized, config.Apikey, config.E2ekey); err != nil {
		fmt.Println("ERROR: Invalid service destination")
		os.Exit(1)
	}
}

// Delete deletes the login information file.
// It will return an error if the file exists but could not be deleted.
func Delete() error {
	return deleteCredentials(getConfigPath())
}

func confirmDestination(answer string) bool {
	return strings.EqualFold(strings.TrimSpace(answer), "yes")
}
