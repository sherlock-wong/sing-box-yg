package app

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sherlock-wong/vps-net-manager/internal/certificate"
	"github.com/sherlock-wong/vps-net-manager/internal/dependency"
	"github.com/sherlock-wong/vps-net-manager/internal/model"
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
	if err := configureACMEOutputPaths(script, certificatePath, keyPath); err != nil {
		return certificate.Info{}, fmt.Errorf("configure ACME output paths: %w", err)
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

// RunInteractive starts the locked ACME script with the administrator's
// terminal attached, then independently verifies the requested output pair.
// It is intentionally separate from Run so non-interactive systemd work can
// never unexpectedly request terminal input.
func (adapter ACMEAdapter) RunInteractive(ctx context.Context, certificatePath, keyPath, hostname string, now time.Time) (certificate.Info, error) {
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
	if err := configureACMEOutputPaths(script, certificatePath, keyPath); err != nil {
		return certificate.Info{}, fmt.Errorf("configure ACME output paths: %w", err)
	}
	if err := platform.RunAttached(ctx, platform.Command{Path: "bash", Args: []string{script}, Timeout: 10 * time.Minute}); err != nil {
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

// configureACMEOutputPaths keeps the verified upstream script but rewrites its
// fixed legacy output location before execution. The certificate is therefore
// never written under /root and remains within the manager's state directory.
func configureACMEOutputPaths(scriptPath, certificatePath, keyPath string) error {
	certificateDirectory := filepath.Dir(certificatePath)
	if err := os.MkdirAll(certificateDirectory, 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(keyPath), 0o700); err != nil {
		return err
	}
	script, err := os.ReadFile(scriptPath)
	if err != nil {
		return err
	}
	configured := strings.ReplaceAll(string(script), "/root/ygkkkca/cert.crt", certificatePath)
	configured = strings.ReplaceAll(configured, "/root/ygkkkca/private.key", keyPath)
	configured = strings.ReplaceAll(configured, "/root/ygkkkca", certificateDirectory)
	configured = strings.ReplaceAll(configured, "case \"$cd\" in \n", "case \"$cd\" in\n")
	// The pinned upstream helper presents an IP certificate path and kills every
	// process on port 80 before HTTP validation. Neither behavior belongs in
	// VPNM: only DNS-name certificates are supported, and unrelated services
	// must never be stopped by certificate setup.
	configured = strings.ReplaceAll(configured, `acme2(){
if [[ -n $(lsof -i :80|grep -v "PID") ]]; then
yellow "检测到80端口被占用，现执行80端口全释放"
sleep 2
lsof -i :80|grep -v "PID"|awk '{print "kill -9",$2}'|sh >/dev/null 2>&1
green "80端口全释放完毕！"
sleep 2
fi
}`, `acme2(){
if [[ -n $(lsof -i :80|grep -v "PID") ]]; then
red "80 端口已被占用；VPNM 不会停止其他服务。请改用 DNS API 验证，或自行暂停占用 80 端口的服务后重试。"
return 1
fi
}`)
	configured = strings.ReplaceAll(configured, `ab="1.选择独立80端口模式申请IP证书（无需域名，小白推荐）\n2.选择独立80端口模式申请域名证书（需域名）\n3.选择DNS API模式申请证书（需域名、ID、Key），自动识别单域名与泛域名\n 请选择："
readp "$ab" cd
case "$cd" in
1 ) acme2 && acme3 && ACMEstandaloneIPcheck;;
2 ) acme2 && acme3 && ACMEstandaloneDNScheck;;
3 ) acme3 && ACMEDNScheck;;
esac`, `ab="1.选择独立80端口模式申请域名证书（需域名）\n2.选择DNS API模式申请证书（需域名、ID、Key），自动识别单域名与泛域名\n 请选择："
readp "$ab" cd
case "$cd" in
1 ) acme2 && acme3 && ACMEstandaloneDNScheck;;
2 ) acme3 && ACMEDNScheck;;
esac`)
	configured = strings.ReplaceAll(configured, "if [[ $domainIP = $v4 ]]; then", "if [[ -n \"$v4\" && $domainIP =~ $v4 ]]; then")
	configured = strings.ReplaceAll(configured, "if [[ $domainIP = $v6 ]]; then", "if [[ -n \"$v6\" && $domainIP =~ $v6 ]]; then")
	return os.WriteFile(scriptPath, []byte(configured), 0o700)
}

// AddInteractiveACMECertificate runs the locked ACME flow and persists the
// resulting verified certificate as a manager-owned, renewable certificate.
// The state/configuration transaction is completed by the caller's normal
// Apply operation; this function only stages the certificate files needed by
// that candidate state.
func AddInteractiveACMECertificate(ctx context.Context, stateDirectory string, state model.State, id, name, certificatePath, keyPath, hostname string, now time.Time) (model.State, error) {
	if _, err := (ACMEAdapter{}).RunInteractive(ctx, certificatePath, keyPath, hostname, now); err != nil {
		return model.State{}, err
	}
	candidate, artifacts, err := StageImportedCertificate(ctx, stateDirectory, state, id, name, certificatePath, keyPath, model.CertificateModeTrusted, true, now)
	if err != nil {
		return model.State{}, err
	}
	if _, err := (FilesystemStore{}).Commit(ctx, artifacts); err != nil {
		return model.State{}, fmt.Errorf("store ACME certificate: %w", err)
	}
	return candidate, nil
}
