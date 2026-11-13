package main

import (
	"github.com/Activer007/SupaShare/internal/environment/flagparser"
	"testing"
)

func TestValidateOIDCLinkFlags(t *testing.T) {
	valid := flagparser.MainFlags{OIDCLink: true, OIDCLinkUser: 7, OIDCSubject: "Case-Sensitive-Subject"}
	if err := validateOIDCLinkFlags(valid); err != nil {
		t.Fatal(err)
	}
	cases := []flagparser.MainFlags{
		{OIDCLink: true},
		{OIDCLink: true, OIDCLinkUser: -1, OIDCSubject: "subject"},
		{OIDCLink: true, OIDCLinkUser: 7},
	}
	for _, mutate := range []func(*flagparser.MainFlags){
		func(f *flagparser.MainFlags) { f.Reconfigure = true },
		func(f *flagparser.MainFlags) { f.DeploymentPassword = "password" },
		func(f *flagparser.MainFlags) { f.CreateSsl = true },
		func(f *flagparser.MainFlags) { f.InstallService = true },
		func(f *flagparser.MainFlags) { f.UninstallService = true },
		func(f *flagparser.MainFlags) { f.ShowVersion = true },
		func(f *flagparser.MainFlags) { f.Migration.DoMigration = true },
	} {
		f := valid
		mutate(&f)
		cases = append(cases, f)
	}
	for _, f := range cases {
		if validateOIDCLinkFlags(f) == nil {
			t.Errorf("invalid flags accepted: %+v", f)
		}
	}
}
