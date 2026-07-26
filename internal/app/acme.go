package app

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/sherlock-wong/vps-net-manager/internal/certificate"
	"github.com/sherlock-wong/vps-net-manager/internal/dependency"
	"github.com/sherlock-wong/vps-net-manager/internal/platform"
)

// ACMEAdapter executes only the lockfile-pinned script. A successful process
// is not sufficient: the requested certificate/private-key pair is parsed and
// SAN-validated afterwards before this method returns success.
type ACMEAdapter struct {
	Locks  dependency.Locks
	Client *http.Client
	Runner platform.CommandRunner
}

func (adapter ACMEAdapter) Run(ctx context.Context, arguments []string, certificatePath, keyPath, hostname string, now time.Time) (certificate.Info, error) {
	if certificatePath == "" || keyPath == "" || hostname == "" {
		return certificate.Info{}, fmt.Errorf("certificate path, key path, and hostname are required")
	}
	locks := adapter.Locks
	if locks.ACME.Commit == "" {
		var err error
		locks, err = dependency.Embedded()
		if err != nil {
			return certificate.Info{}, err
		}
	}
	if err := locks.Validate(); err != nil {
		return certificate.Info{}, err
	}
	stage, err := os.MkdirTemp("", "vpnm-acme-")
	if err != nil {
		return certificate.Info{}, err
	}
	defer os.RemoveAll(stage)
	script := filepath.Join(stage, "acme.sh")
	if err := platform.DownloadVerified(ctx, adapter.Client, platform.Download{URL: locks.ACME.URL, SHA256: locks.ACME.SHA256, Mode: 0o700}, script); err != nil {
		return certificate.Info{}, fmt.Errorf("download locked ACME script: %w", err)
	}
	runner := adapter.Runner
	if runner == nil {
		runner = platform.SystemRunner{}
	}
	if _, err := runner.Run(ctx, platform.Command{Path: "bash", Args: append([]string{script}, arguments...), Timeout: 10 * time.Minute}); err != nil {
		return certificate.Info{}, fmt.Errorf("run ACME script: %w", err)
	}
	certificatePEM, err := os.ReadFile(certificatePath)
	if err != nil {
		return certificate.Info{}, fmt.Errorf("read ACME certificate: %w", err)
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return certificate.Info{}, fmt.Errorf("read ACME key: %w", err)
	}
	info, err := certificate.Inspect(certificatePEM, keyPEM, hostname, now)
	if err != nil {
		return certificate.Info{}, fmt.Errorf("validate ACME certificate: %w", err)
	}
	return info, nil
}
