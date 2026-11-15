package environment

import "testing"

func TestN15NewCapabilityLength(t *testing.T) {
	for _, n := range []string{"0", "5", "8", "15", "21", "22", "40"} {
		t.Run(n, func(t *testing.T) {
			t.Setenv("GOKAPI_LENGTH_ID", n)
			t.Setenv("GOKAPI_LENGTH_HOTLINK_ID", n)
			env := New()
			if env.LengthId < 22 || env.LengthHotlinkId < 22 {
				t.Fatal("weak new identifier")
			}
		})
	}
	if MinLengthId != 5 {
		t.Fatal("legacy read compatibility changed")
	}
}
