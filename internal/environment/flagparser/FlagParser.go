package flagparser

import (
	"errors"
	"flag"
	"fmt"
	"github.com/Activer007/SupaShare/internal/secretinput"
	"io"
	"os"
)

// ParseFlags reads the passed program arguments
func ParseFlags() MainFlags { return parseFlags(true) }

// ParseEnvironmentFlags reads only the nonsecret startup projection. Environment
// construction happens repeatedly; it must never prompt or reread secret files.
func ParseEnvironmentFlags() MainFlags { return parseFlags(false) }

func parseFlags(resolveSecrets bool) MainFlags {

	var aliases []alias

	// Is disabled during testing, as otherwise it will raise an error if called in the test's init() function,
	// which replaces the arguments
	if DisableParsing {
		return MainFlags{}
	}
	flags, ok := parseMigrationWithSecrets(resolveSecrets)
	if ok {
		return flags
	}

	passedFlags := flag.FlagSet{}
	passedFlags.SetOutput(io.Discard)
	versionFlagLong := passedFlags.Bool("version", false, "Show version info")
	versionFlagShort := passedFlags.Bool("v", false, "alias")
	aliases = append(aliases, alias{
		Long:  "version",
		Short: "v",
	})
	reconfigureFlag := passedFlags.Bool("reconfigure", false, "Runs setup again to change SupaShare configuration / passwords")
	createSslFlag := passedFlags.Bool("create-ssl", false, "Creates a new SSL certificate valid for 365 days")
	configPathFlagLong := passedFlags.String("config", "", "Use provided config file instead of the default one in the config directory.\n"+
		"                               Can point to a different directory than default or the one provided by -config-path or env variable GOKAPI_CONFIG_DIR")
	configPathFlagShort := passedFlags.String("c", "", "alias")
	aliases = append(aliases, alias{
		Long:  "config",
		Short: "c",
	})
	configDirFlagLong := passedFlags.String("config-dir", "", "Sets the config directory. Same as env variable GOKAPI_CONFIG_DIR")
	configDirFlagShort := passedFlags.String("cd", "", "alias")
	aliases = append(aliases, alias{
		Long:  "config-dir",
		Short: "cd",
	})
	dataDirFlagLong := passedFlags.String("data", "", "Sets the data directory. Same as env variable GOKAPI_DATA_DIR")
	dataDirFlagShort := passedFlags.String("d", "", "alias")
	aliases = append(aliases, alias{
		Long:  "data",
		Short: "d",
	})
	portFlagLong := passedFlags.Int("port", 0, "Sets the port for setup. Same as env variable GOKAPI_PORT")
	portFlagShort := passedFlags.Int("p", 0, "alias")
	aliases = append(aliases, alias{
		Long:  "port",
		Short: "p",
	})
	disableCorsCheck := passedFlags.Bool("disable-cors-check", false, "Disables the CORS check on startup")

	installService := passedFlags.Bool("install-service", false, "Installs SupaShare as a systemd service")
	uninstallService := passedFlags.Bool("uninstall-service", false, "Uninstalls the SupaShare systemd service")
	oidcLinkUser := passedFlags.Int("oidc-link-user", 0, "Explicitly bind an existing local user ID to an OIDC subject and exit (stop server first)")
	oidcSubject := passedFlags.String("oidc-subject", "", "Exact subject from the configured identity provider; requires --oidc-link-user")
	deploymentPassword := addSecretFlags(&passedFlags, "deployment-password", "Deployment administrator password")

	passedFlags.Usage = showUsage(passedFlags, aliases)
	err := passedFlags.Parse(os.Args[1:])

	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		fmt.Fprintln(os.Stderr, "Invalid arguments; use --help for supported options")
		os.Exit(2)
	}
	if passedFlags.NArg() != 0 {
		failSecretArguments()
	}
	password := ""
	deploymentPassword.validateSources()
	if resolveSecrets && !*versionFlagShort && !*versionFlagLong {
		password = deploymentPassword.resolve("Deployment administrator password")
	}

	result := MainFlags{
		ShowVersion:        *versionFlagShort || *versionFlagLong,
		Reconfigure:        *reconfigureFlag,
		CreateSsl:          *createSslFlag,
		ConfigPath:         getAliasedString(configPathFlagLong, configPathFlagShort),
		ConfigDir:          getAliasedString(configDirFlagLong, configDirFlagShort),
		DataDir:            getAliasedString(dataDirFlagLong, dataDirFlagShort),
		Port:               getAliasedInt(portFlagLong, portFlagShort),
		DisableCorsCheck:   *disableCorsCheck,
		InstallService:     *installService,
		UninstallService:   *uninstallService,
		DeploymentPassword: password,
		OIDCLinkUser:       *oidcLinkUser,
		OIDCSubject:        *oidcSubject,
	}
	passedFlags.Visit(func(f *flag.Flag) {
		if f.Name == "oidc-link-user" || f.Name == "oidc-subject" {
			result.OIDCLink = true
		}
	})
	result.setBoolValues()
	return result
}

func parseMigration() (MainFlags, bool) { return parseMigrationWithSecrets(true) }

func parseMigrationWithSecrets(resolveSecrets bool) (MainFlags, bool) {
	if len(os.Args) > 1 && os.Args[1] == "migrate-database" {
		migrateFlags := parseMigrateFlagsWithSecrets(os.Args[2:], resolveSecrets)
		return MainFlags{
			Migration: migrateFlags}, true
	}
	return MainFlags{}, false
}

func parseMigrateFlags(args []string) MigrateFlags { return parseMigrateFlagsWithSecrets(args, true) }

func parseMigrateFlagsWithSecrets(args []string, resolveSecrets bool) MigrateFlags {
	fs := flag.NewFlagSet("migrate-database", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	source := addSecretFlags(fs, "source", "Source database URL")
	destination := addSecretFlags(fs, "destination", "Destination database URL")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: migrate-database --source-file FILE --destination-file FILE (or --source-prompt / --destination-prompt)")
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		failSecretArguments()
	}
	if fs.NArg() != 0 {
		failSecretArguments()
	}
	source.validateSources()
	destination.validateSources()
	if !source.present() || !destination.present() {
		fmt.Fprintln(os.Stderr, "Source and destination are required; use protected files or hidden prompts")
		os.Exit(1)
	}
	result := MigrateFlags{DoMigration: true}
	if !resolveSecrets {
		return result
	}
	result.Source = source.resolve("Source database URL")
	result.Destination = destination.resolve("Destination database URL")
	if result.Source == "" || result.Destination == "" {
		fmt.Fprintln(os.Stderr, "Source and destination are required; use protected files or hidden prompts")
		os.Exit(1)
	}
	if result.Source == result.Destination {
		fmt.Fprintln(os.Stderr, "Source and destination must differ")
		os.Exit(1)
	}
	return result
}

// Secret values must never enter flag's error formatter. Its output is discarded,
// and repeated values fail before any file or terminal is read.
type secretValue struct {
	value string
	set   bool
}

func (s *secretValue) String() string { return "" }
func (s *secretValue) Set(value string) error {
	if s.set {
		return errors.New("repeated secret source")
	}
	s.value, s.set = value, true
	return nil
}

// A prompt is a source selector, not a repeatable toggle.
type secretPrompt struct{ value, set bool }

func (s *secretPrompt) String() string   { return "false" }
func (s *secretPrompt) IsBoolFlag() bool { return true }
func (s *secretPrompt) Set(value string) error {
	if s.set || value != "true" {
		return errors.New("invalid or repeated prompt selector")
	}
	s.value, s.set = true, true
	return nil
}

type secretFlags struct {
	inline, file secretValue
	prompt       secretPrompt
}

func addSecretFlags(fs *flag.FlagSet, name, label string) *secretFlags {
	s := &secretFlags{}
	fs.Var(&s.inline, name, "Deprecated argv secret; prefer --"+name+"-file or --"+name+"-prompt")
	fs.Var(&s.file, name+"-file", label+" from a protected private file")
	fs.Var(&s.prompt, name+"-prompt", label+" from hidden interactive terminal input")
	return s
}
func (s *secretFlags) present() bool { return s.inline.set || s.file.set || s.prompt.value }

func (s *secretFlags) validateSources() {
	count := 0
	if s.inline.set {
		count++
	}
	if s.file.set {
		count++
	}
	if s.prompt.value {
		count++
	}
	if count > 1 {
		failSecretArguments()
	}
}
func (s *secretFlags) resolve(label string) string {
	value, err := secretinput.Resolve(secretinput.Source{Inline: s.inline.value, File: s.file.value, InlineSet: s.inline.set, FileSet: s.file.set, Prompt: s.prompt.value}, label)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Unable to read "+label+": "+err.Error())
		os.Exit(2)
	}
	return value
}
func failSecretArguments() {
	fmt.Fprintln(os.Stderr, "Invalid or repeated arguments; use --help for protected-input options")
	os.Exit(2)
}

// MigrateFlags contains flags passed if migration is requested
type MigrateFlags struct {
	DoMigration bool
	Source      string
	Destination string
}

func showUsage(mainFlags flag.FlagSet, aliases []alias) func() {
	return func() {
		fmt.Print("Usage:\n\n")
		mainFlags.VisitAll(func(f *flag.Flag) {
			if isAlias(f.Name, aliases) {
				return
			}
			output := "--" + f.Name
			aliasExists, aliasName := hasAlias(f.Name, aliases)
			if aliasExists {
				output = "-" + aliasName + ", " + output
			}
			if f.DefValue == "" {
				output = output + " <string>"
			}
			if f.DefValue == "0" {
				output = output + " <int>"
			}
			fmt.Printf("%-30s %s\n", output, f.Usage)
		})
		fmt.Printf("\n%-30s %s\n", "migrate-database", "Migrate an old database to a new database (e.g. SQLite to Redis)")
		fmt.Printf("%-30s %s\n", "--source-file", "Original database URL in a protected file")
		fmt.Printf("%-30s %s\n", "--destination-file", "New database URL in a protected file")

	}
}

func getAliasedString(flag1, flag2 *string) string {
	if *flag1 != "" {
		return *flag1
	}
	return *flag2
}
func getAliasedInt(flag1, flag2 *int) int {
	if *flag1 != 0 {
		return *flag1
	}
	return *flag2
}

// MainFlags holds info for the parsed program arguments
type MainFlags struct {
	OIDCLink           bool
	OIDCLinkUser       int
	OIDCSubject        string
	ConfigPath         string
	ConfigDir          string
	DataDir            string
	DatabaseUrl        string
	DeploymentPassword string
	ShowVersion        bool
	Reconfigure        bool
	CreateSsl          bool
	IsConfigPathSet    bool
	IsConfigDirSet     bool
	IsDataDirSet       bool
	IsPortSet          bool
	IsDatabaseUrlSet   bool
	DisableCorsCheck   bool
	InstallService     bool
	UninstallService   bool
	Port               int
	Migration          MigrateFlags
}

func (mf *MainFlags) setBoolValues() {
	mf.IsConfigPathSet = mf.ConfigPath != ""
	mf.IsConfigDirSet = mf.ConfigDir != ""
	mf.IsDataDirSet = mf.DataDir != ""
	mf.IsPortSet = mf.Port != 0
	mf.IsDatabaseUrlSet = mf.DatabaseUrl != ""
}

type alias struct {
	Long  string
	Short string
}

func isAlias(value string, aliases []alias) bool {
	for _, name := range aliases {
		if name.Short == value {
			return true
		}
	}
	return false
}
func hasAlias(value string, aliases []alias) (bool, string) {
	for _, name := range aliases {
		if name.Long == value {
			return true, name.Short
		}
	}
	return false, ""
}
