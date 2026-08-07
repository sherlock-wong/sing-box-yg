package dependency

import "testing"

func TestEmbeddedLocksAreValidAndCoverSupportedArchitectures(t *testing.T) {
	locks, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"sing-box", "xray", "realm", "komari"} {
		for _, architecture := range []string{"amd64", "arm64"} {
			_, asset, err := locks.Asset(name, architecture)
			if err != nil || asset.URL == "" {
				t.Fatalf("%s/%s = %#v, %v", name, architecture, asset, err)
			}
		}
	}
}
