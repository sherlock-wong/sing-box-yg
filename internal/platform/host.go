package platform

import (
	"fmt"
	"os"
	"runtime"
	"strings"
)

type Host struct {
	Distribution string
	Architecture string
}

// InspectSupportedHost applies the deliberately narrow production support
// contract: Debian/Ubuntu, systemd, and Linux amd64/arm64 only.
func InspectSupportedHost() (Host, error) {
	contents, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return Host{}, fmt.Errorf("read os-release: %w", err)
	}
	_, err = os.Stat("/run/systemd/system")
	return ValidateHost(contents, runtime.GOOS, runtime.GOARCH, err == nil)
}

func ValidateHost(osRelease []byte, goos, architecture string, systemd bool) (Host, error) {
	if goos != "linux" {
		return Host{}, fmt.Errorf("仅支持 Linux Debian/Ubuntu")
	}
	id := osReleaseValue(string(osRelease), "ID")
	if id != "debian" && id != "ubuntu" {
		return Host{}, fmt.Errorf("仅支持 Debian 或 Ubuntu")
	}
	if !systemd {
		return Host{}, fmt.Errorf("需要 systemd")
	}
	if architecture != "amd64" && architecture != "arm64" {
		return Host{}, fmt.Errorf("仅支持 amd64 或 arm64")
	}
	return Host{Distribution: id, Architecture: architecture}, nil
}

func osReleaseValue(contents, key string) string {
	for _, line := range strings.Split(contents, "\n") {
		name, value, found := strings.Cut(line, "=")
		if !found || name != key {
			continue
		}
		return strings.Trim(value, "\"'")
	}
	return ""
}
