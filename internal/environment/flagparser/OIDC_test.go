package flagparser

import (
	"os"
	"testing"
)

func TestOIDCLinkParsing(t *testing.T) {
	oldArgs, oldDisable := os.Args, DisableParsing
	defer func() { os.Args = oldArgs; DisableParsing = oldDisable }()
	DisableParsing = false
	for _, args := range [][]string{
		{"--oidc-link-user", "7", "--oidc-subject", "Case-Sensitive-Subject", "--config", "/tmp/example.json"},
		{"--oidc-link-user", "0"},
		{"--oidc-subject", ""},
	} {
		os.Args = append([]string{"supashare"}, args...)
		flags := ParseFlags()
		if !flags.OIDCLink {
			t.Fatal("explicit incomplete command must not fall through to server startup")
		}
		if len(args) > 2 && (flags.OIDCLinkUser != 7 || flags.OIDCSubject != "Case-Sensitive-Subject" || flags.ConfigPath != "/tmp/example.json") {
			t.Fatalf("unexpected flags: %+v", flags)
		}
	}
}
