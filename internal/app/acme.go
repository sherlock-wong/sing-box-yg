package app

import (
	"bufio"
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
	"golang.org/x/term"
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

// RunInteractive uses the official, lockfile-pinned acme.sh client. Its
// prompts are deliberately limited to domain validation methods supported by
// VPNM; no unrelated installer menu or IP-certificate flow is exposed.
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
	stateDirectory, err := acmeStateDirectory(certificatePath)
	if err != nil {
		return certificate.Info{}, err
	}
	clientDirectory := filepath.Join(stateDirectory, "acme-client")
	if err := os.MkdirAll(clientDirectory, 0o700); err != nil {
		return certificate.Info{}, err
	}
	script := filepath.Join(clientDirectory, "acme.sh")
	if err := platform.DownloadVerified(ctx, adapter.Client, platform.Download{URL: locks.ACME.URL, SHA256: locks.ACME.SHA256, Mode: 0o700}, script); err != nil {
		return certificate.Info{}, fmt.Errorf("download locked ACME script: %w", err)
	}
	if err := ensureACMECloudflareHook(ctx, adapter.Client, locks, clientDirectory); err != nil {
		return certificate.Info{}, err
	}
	home := filepath.Join(clientDirectory, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		return certificate.Info{}, err
	}
	reader := bufio.NewReader(os.Stdin)
	fmt.Fprint(os.Stdout, "验证方式：1 Cloudflare DNS API（推荐）  2 HTTP-01 独立 80 端口  0 取消：")
	method, err := readACMEInput(reader)
	if err != nil || method == "0" || method == "" {
		return certificate.Info{}, fmt.Errorf("ACME 申请已取消")
	}
	fmt.Fprint(os.Stdout, "ACME 注册邮箱（回车跳过）：")
	email, err := readACMEInput(reader)
	if err != nil {
		return certificate.Info{}, err
	}
	base := []string{script, "--home", home, "--config-home", home, "--cert-home", home}
	register := append(append([]string{}, base...), "--register-account", "--server", "letsencrypt")
	if email != "" {
		register = append(register, "-m", email)
	}
	if result, err := platform.Run(ctx, platform.Command{Path: "bash", Args: register, Timeout: 3 * time.Minute}); err != nil {
		return certificate.Info{}, fmt.Errorf("register ACME account: %s: %w", strings.TrimSpace(result.Output), err)
	}
	issue := append(append([]string{}, base...), "--issue", "-d", hostname, "--ecc", "--server", "letsencrypt")
	command := platform.Command{Path: "bash", Args: issue, Timeout: 10 * time.Minute}
	switch method {
	case "1":
		fmt.Fprint(os.Stdout, "Cloudflare Account ID：")
		accountID, err := readACMEInput(reader)
		if err != nil || accountID == "" {
			return certificate.Info{}, fmt.Errorf("Cloudflare Account ID 不能为空")
		}
		fmt.Fprint(os.Stdout, "Cloudflare DNS API Token（输入不回显，请勿发送到聊天）：")
		token, err := readACMESecret(reader)
		if err != nil || token == "" {
			return certificate.Info{}, fmt.Errorf("Cloudflare DNS API Token 不能为空")
		}
		command.Args = append(command.Args, "--dns", "dns_cf")
		command.Env = []string{"CF_Account_ID=" + accountID, "CF_Token=" + token}
		command.Redact = []string{token}
	case "2":
		command.Args = append(command.Args, "--standalone")
	default:
		return certificate.Info{}, fmt.Errorf("无效验证方式")
	}
	issuedFullchain := filepath.Join(home, hostname+"_ecc", "fullchain.cer")
	if _, err := os.Stat(issuedFullchain); os.IsNotExist(err) {
		if result, err := platform.Run(ctx, command); err != nil {
			return certificate.Info{}, fmt.Errorf("issue ACME certificate: %s: %w", strings.TrimSpace(result.Output), err)
		}
	} else if err != nil {
		return certificate.Info{}, err
	}
	if err := os.MkdirAll(filepath.Dir(certificatePath), 0o700); err != nil {
		return certificate.Info{}, fmt.Errorf("create ACME certificate output directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(keyPath), 0o700); err != nil {
		return certificate.Info{}, fmt.Errorf("create ACME key output directory: %w", err)
	}
	install := append(append([]string{}, base...), "--install-cert", "-d", hostname, "--ecc", "--key-file", keyPath, "--fullchain-file", certificatePath)
	if result, err := platform.Run(ctx, platform.Command{Path: "bash", Args: install, Timeout: 3 * time.Minute}); err != nil {
		return certificate.Info{}, fmt.Errorf("install ACME certificate: %s: %w", strings.TrimSpace(result.Output), err)
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

func acmeStateDirectory(certificatePath string) (string, error) {
	directory := filepath.Clean(filepath.Dir(certificatePath))
	if filepath.Base(filepath.Dir(directory)) != "acme" {
		return "", fmt.Errorf("ACME certificate path must be under an acme directory")
	}
	return filepath.Dir(filepath.Dir(directory)), nil
}

func readACMEInput(reader *bufio.Reader) (string, error) {
	value, err := reader.ReadString('\n')
	if err != nil && len(value) == 0 {
		return "", err
	}
	return strings.TrimSpace(value), nil
}

func readACMESecret(reader *bufio.Reader) (string, error) {
	if term.IsTerminal(int(os.Stdin.Fd())) {
		value, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stdout)
		return strings.TrimSpace(string(value)), err
	}
	return readACMEInput(reader)
}

// UpdateCloudflareCredentials replaces the credentials acme.sh will use for
// subsequent DNS-01 renewals. They are shared by certificates in this VPNM
// ACME home and never written to VPNM state.json.
func UpdateCloudflareCredentials(stateDirectory, accountID, token string) error {
	if accountID == "" || token == "" || strings.ContainsAny(accountID, "'\r\n") || strings.ContainsAny(token, "'\r\n") {
		return fmt.Errorf("invalid Cloudflare credentials")
	}
	path := filepath.Join(stateDirectory, "acme-client", "home", "account.conf")
	contents, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read ACME account configuration: %w", err)
	}
	contents = replaceACMEAccountSetting(contents, "SAVED_CF_Account_ID", accountID)
	contents = replaceACMEAccountSetting(contents, "SAVED_CF_Token", token)
	if err := platform.AtomicWriteFile(path, contents, 0o600); err != nil {
		return fmt.Errorf("store ACME Cloudflare credentials: %w", err)
	}
	return nil
}

func replaceACMEAccountSetting(contents []byte, key, value string) []byte {
	lines := strings.Split(strings.TrimRight(string(contents), "\n"), "\n")
	needle := key + "="
	updated := false
	for index, line := range lines {
		if strings.HasPrefix(line, needle) {
			lines[index] = needle + "'" + value + "'"
			updated = true
		}
	}
	if !updated {
		lines = append(lines, needle+"'"+value+"'")
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}

// Renew runs acme.sh's non-interactive renewal pass. Certificate deployment
// paths were registered during issuance; callers should sync the resulting
// managed source files afterwards.
func (adapter ACMEAdapter) Renew(ctx context.Context, stateDirectory string) error {
	locks := adapter.Locks
	if locks.ACME.Commit == "" {
		var err error
		locks, err = dependency.Embedded()
		if err != nil {
			return err
		}
	}
	if err := locks.Validate(); err != nil {
		return err
	}
	clientDirectory := filepath.Join(stateDirectory, "acme-client")
	if err := os.MkdirAll(clientDirectory, 0o700); err != nil {
		return err
	}
	script := filepath.Join(clientDirectory, "acme.sh")
	if _, err := os.Stat(script); os.IsNotExist(err) {
		if err := platform.DownloadVerified(ctx, adapter.Client, platform.Download{URL: locks.ACME.URL, SHA256: locks.ACME.SHA256, Mode: 0o700}, script); err != nil {
			return fmt.Errorf("download locked ACME script: %w", err)
		}
	} else if err != nil {
		return err
	}
	if err := ensureACMECloudflareHook(ctx, adapter.Client, locks, clientDirectory); err != nil {
		return err
	}
	home := filepath.Join(clientDirectory, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		return err
	}
	runner := adapter.Runner
	if runner == nil {
		runner = platform.SystemRunner{}
	}
	if _, err := runner.Run(ctx, platform.Command{Path: "bash", Args: []string{script, "--home", home, "--config-home", home, "--cert-home", home, "--cron"}, Timeout: 10 * time.Minute}); err != nil {
		return fmt.Errorf("renew ACME certificates: %w", err)
	}
	return nil
}

func ensureACMECloudflareHook(ctx context.Context, client *http.Client, locks dependency.Locks, clientDirectory string) error {
	if locks.ACMECloudflare.Commit == "" {
		return fmt.Errorf("ACME Cloudflare DNS hook is not locked")
	}
	directory := filepath.Join(clientDirectory, "dnsapi")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	if err := platform.DownloadVerified(ctx, client, platform.Download{URL: locks.ACMECloudflare.URL, SHA256: locks.ACMECloudflare.SHA256, Mode: 0o700}, filepath.Join(directory, "dns_cf.sh")); err != nil {
		return fmt.Errorf("download locked ACME Cloudflare DNS hook: %w", err)
	}
	return nil
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
