package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/sherlock-wong/vps-net-manager/internal/certificate"
	"github.com/sherlock-wong/vps-net-manager/internal/model"
	"github.com/sherlock-wong/vps-net-manager/internal/platform"
	"github.com/sherlock-wong/vps-net-manager/internal/protocol"
	"github.com/sherlock-wong/vps-net-manager/internal/web"
)

const DefaultWebStateFile = "web.json"
const DefaultNginxVPNMConfig = "/etc/nginx/conf.d/vps-net-manager.conf"

type NginxInstaller struct {
	Runner     platform.CommandRunner
	ConfigPath string
}

func (installer NginxInstaller) Installed(ctx context.Context) bool {
	runner := installer.Runner
	if runner == nil {
		runner = platform.SystemRunner{}
	}
	_, err := runner.Run(ctx, platform.Command{Path: "nginx", Args: []string{"-v"}, Timeout: 15 * time.Second})
	return err == nil
}

func (installer NginxInstaller) Install(ctx context.Context) error {
	runner := installer.Runner
	if runner == nil {
		runner = platform.SystemRunner{}
	}
	if installer.Installed(ctx) {
		return nil
	}
	if _, err := runner.Run(ctx, platform.Command{Path: "apt-get", Args: []string{"update"}, Timeout: 3 * time.Minute}); err != nil {
		return fmt.Errorf("update APT index: %w", err)
	}
	if _, err := runner.Run(ctx, platform.Command{Path: "apt-get", Args: []string{"install", "-y", "nginx"}, Timeout: 5 * time.Minute}); err != nil {
		return fmt.Errorf("install nginx: %w", err)
	}
	return nil
}

// UnmanagedConfigFiles lists active site snippets outside VPNM's dedicated
// include. It is presented before package removal, not used to delete them.
func (installer NginxInstaller) UnmanagedConfigFiles() ([]string, error) {
	managed := filepath.Clean(installer.configPath())
	var files []string
	for _, directory := range []string{"/etc/nginx/sites-enabled", "/etc/nginx/conf.d"} {
		entries, err := os.ReadDir(directory)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read Nginx configuration directory %s: %w", directory, err)
		}
		for _, entry := range entries {
			path := filepath.Join(directory, entry.Name())
			if filepath.Clean(path) != managed {
				files = append(files, path)
			}
		}
	}
	sort.Strings(files)
	return files, nil
}

// Uninstall removes the nginx packages installed by VPNM's supported APT
// workflow. The caller must obtain explicit confirmation after displaying
// UnmanagedConfigFiles; unrelated site snippets are never deleted here.
func (installer NginxInstaller) Uninstall(ctx context.Context) error {
	runner := installer.Runner
	if runner == nil {
		runner = platform.SystemRunner{}
	}
	if !installer.Installed(ctx) {
		return fmt.Errorf("Nginx is not installed")
	}
	if _, err := runner.Run(ctx, platform.Command{Path: "systemctl", Args: []string{"disable", "--now", "nginx"}, Timeout: 45 * time.Second}); err != nil {
		return fmt.Errorf("stop nginx: %w", err)
	}
	if err := os.Remove(installer.configPath()); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove VPNM nginx configuration: %w", err)
	}
	if _, err := runner.Run(ctx, platform.Command{Path: "apt-get", Args: []string{"purge", "-y", "nginx", "nginx-common"}, Timeout: 5 * time.Minute}); err != nil {
		return fmt.Errorf("purge nginx: %w", err)
	}
	return nil
}

func (installer NginxInstaller) configPath() string {
	if installer.ConfigPath != "" {
		return installer.ConfigPath
	}
	return DefaultNginxVPNMConfig
}

type WebApplyOptions struct {
	StateDirectory, NginxConfigPath string
	Firewall                        FirewallController
	Artifacts                       ArtifactStore
	Runner                          platform.CommandRunner
}

func DefaultWebApplyOptions(stateDirectory string) WebApplyOptions {
	return WebApplyOptions{StateDirectory: stateDirectory, NginxConfigPath: DefaultNginxVPNMConfig, Firewall: UFWController{}, Artifacts: FilesystemStore{}}
}

func LoadWebState(path string, certificates map[string]model.Certificate) (web.State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return web.State{}, fmt.Errorf("read web state: %w", err)
	}
	return web.Parse(data, certificates)
}

// LoadWebStateOrEmpty returns a usable empty proxy document before the first
// managed web service is configured. It does not create a file by itself.
func LoadWebStateOrEmpty(path string, certificates map[string]model.Certificate) (web.State, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return web.NewState(), nil
	}
	if err != nil {
		return web.State{}, fmt.Errorf("read web state: %w", err)
	}
	return web.Parse(data, certificates)
}

func (options WebApplyOptions) Apply(ctx context.Context, previous, candidate web.State, certificates map[string]model.Certificate) error {
	if options.StateDirectory == "" || options.Firewall == nil || options.Artifacts == nil {
		return fmt.Errorf("web apply dependencies are required")
	}
	if options.NginxConfigPath == "" {
		options.NginxConfigPath = DefaultNginxVPNMConfig
	}
	lock, err := platform.AcquireLock(ctx, filepath.Join(options.StateDirectory, "state.lock"))
	if err != nil {
		return fmt.Errorf("acquire state lock: %w", err)
	}
	defer lock.Release()
	if err := candidate.Validate(certificates); err != nil {
		return err
	}
	if err := checkNewWebPorts(previous, candidate); err != nil {
		return err
	}
	if err := validateWebCertificates(candidate, certificates); err != nil {
		return err
	}
	config, err := web.Config(candidate, certificates)
	if err != nil {
		return err
	}
	rules := webFirewallRules(candidate)
	previousRules := webFirewallRules(previous)
	rollbackFW, err := options.Firewall.Prepare(ctx, previousRules, rules)
	if err != nil {
		return fmt.Errorf("pre-open web firewall rules: %w", err)
	}
	encoded, err := json.MarshalIndent(candidate, "", "  ")
	if err != nil {
		return joinFailure(err, rollbackFW(ctx))
	}
	rollbackFiles, err := options.Artifacts.Commit(ctx, []Artifact{{Path: filepath.Join(options.StateDirectory, DefaultWebStateFile), Contents: encoded, Mode: 0o600}, {Path: options.NginxConfigPath, Contents: config, Mode: 0o644}})
	if err != nil {
		return joinFailure(fmt.Errorf("commit web configuration: %w", err), rollbackFW(ctx))
	}
	runner := options.Runner
	if runner == nil {
		runner = platform.SystemRunner{}
	}
	nginx := NginxInstaller{Runner: runner}
	if !nginx.Installed(ctx) {
		if len(candidate.Proxies) == 0 {
			if err := options.Firewall.Finalize(ctx, previousRules, rules); err != nil {
				return joinFailure(fmt.Errorf("finalize web firewall rules: %w", err), rollbackFiles(ctx), rollbackFW(ctx))
			}
			return nil
		}
		return joinFailure(fmt.Errorf("Nginx is not installed"), rollbackFiles(ctx), rollbackFW(ctx))
	}
	if _, err := runner.Run(ctx, platform.Command{Path: "nginx", Args: []string{"-t"}, Timeout: 30 * time.Second}); err != nil {
		return joinFailure(fmt.Errorf("validate nginx configuration: %w", err), rollbackFiles(ctx), rollbackFW(ctx))
	}
	if _, err := runner.Run(ctx, platform.Command{Path: "systemctl", Args: []string{"enable", "--now", "nginx"}, Timeout: 45 * time.Second}); err != nil {
		return joinFailure(fmt.Errorf("enable nginx: %w", err), rollbackFiles(ctx), rollbackFW(ctx))
	}
	if _, err := runner.Run(ctx, platform.Command{Path: "systemctl", Args: []string{"reload", "nginx"}, Timeout: 45 * time.Second}); err != nil {
		return joinFailure(fmt.Errorf("reload nginx: %w", err), rollbackFiles(ctx), rollbackFW(ctx))
	}
	if err := options.Firewall.Finalize(ctx, previousRules, rules); err != nil {
		return joinFailure(fmt.Errorf("finalize web firewall rules: %w", err), rollbackFiles(ctx), rollbackFW(ctx))
	}
	return nil
}

// checkNewWebPorts reserves only ports which are not already owned by a
// previous VPNM proxy. This rejects collisions with protocol cores, Realm, or
// an administrator's pre-existing Nginx site before we write any artifacts.
func checkNewWebPorts(previous, candidate web.State) error {
	known := make(map[uint16]struct{})
	for _, rule := range web.Rules(previous) {
		known[rule.Port] = struct{}{}
	}
	for _, rule := range web.Rules(candidate) {
		if _, found := known[rule.Port]; found {
			continue
		}
		listener, err := net.Listen("tcp", ":"+strconv.Itoa(int(rule.Port)))
		if err != nil {
			return fmt.Errorf("Nginx HTTPS port %d is already in use: %w", rule.Port, err)
		}
		if err := listener.Close(); err != nil {
			return fmt.Errorf("release Nginx HTTPS port %d: %w", rule.Port, err)
		}
	}
	return nil
}

func webFirewallRules(state web.State) []protocol.FirewallRule {
	result := make([]protocol.FirewallRule, 0)
	for _, rule := range web.Rules(state) {
		result = append(result, protocol.FirewallRule{Network: "tcp", Port: rule.Port})
	}
	return result
}

func validateWebCertificates(state web.State, certificates map[string]model.Certificate) error {
	for _, proxy := range state.Proxies {
		item := certificates[proxy.CertificateID]
		certPEM, certErr := os.ReadFile(item.Cert)
		keyPEM, keyErr := os.ReadFile(item.Key)
		if certErr != nil || keyErr != nil {
			return fmt.Errorf("read certificate %s: %w", proxy.CertificateID, firstError(certErr, keyErr))
		}
		if _, err := certificate.Inspect(certPEM, keyPEM, proxy.Domain, time.Now()); err != nil {
			return fmt.Errorf("certificate %s does not cover %s: %w", proxy.CertificateID, proxy.Domain, err)
		}
	}
	return nil
}
func firstError(values ...error) error {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}
