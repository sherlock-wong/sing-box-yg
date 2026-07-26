package platform

import "testing"

func TestValidateHostAcceptsSupportedCombinations(t *testing.T) {
	host, err := ValidateHost([]byte("NAME=Ubuntu\nID=ubuntu\n"), "linux", "arm64", true)
	if err != nil {
		t.Fatal(err)
	}
	if host.Distribution != "ubuntu" || host.Architecture != "arm64" {
		t.Fatalf("host = %+v", host)
	}
}

func TestValidateHostRejectsUnsupportedHost(t *testing.T) {
	for _, test := range []struct {
		os, arch string
		systemd  bool
	}{{"darwin", "arm64", true}, {"linux", "386", true}, {"linux", "amd64", false}} {
		if _, err := ValidateHost([]byte("ID=debian\n"), test.os, test.arch, test.systemd); err == nil {
			t.Fatalf("ValidateHost accepted %#v", test)
		}
	}
	if _, err := ValidateHost([]byte("ID=alpine\n"), "linux", "amd64", true); err == nil {
		t.Fatal("ValidateHost accepted alpine")
	}
}
