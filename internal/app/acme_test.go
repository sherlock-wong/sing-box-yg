package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sherlock-wong/vps-net-manager/internal/certificate"
	"github.com/sherlock-wong/vps-net-manager/internal/dependency"
	"github.com/sherlock-wong/vps-net-manager/internal/platform"
)

func TestACMEAdapterValidatesMaterialAfterScript(t *testing.T) {
	now := time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC)
	certificatePEM, keyPEM, _, err := certificate.CreatePinned("node.example.com", now)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	certPath, keyPath := filepath.Join(directory, "cert.pem"), filepath.Join(directory, "key.pem")
	if err := os.WriteFile(certPath, certificatePEM, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	script := []byte("#!/usr/bin/env bash\nexit 0\n")
	digest := sha256.Sum256(script)
	locks := testLocksWithACME("https://downloads.example.test/acme.sh", hex.EncodeToString(digest[:]))
	adapter := ACMEAdapter{Locks: locks, Client: &http.Client{Transport: archiveTransport{"https://downloads.example.test/acme.sh": script}}, Runner: platform.RunnerFunc(func(context.Context, platform.Command) (platform.CommandResult, error) {
		return platform.CommandResult{}, nil
	})}
	if _, err := adapter.Run(context.Background(), nil, certPath, keyPath, "node.example.com", now); err != nil {
		t.Fatal(err)
	}
}

func TestConfigureACMEOutputPathsRemovesLegacyDirectory(t *testing.T) {
	directory := t.TempDir()
	scriptPath := filepath.Join(directory, "acme.sh")
	legacyScript := []byte("mkdir -p /root/ygkkkca\ncp x /root/ygkkkca/cert.crt\ncp y /root/ygkkkca/private.key\nrm -rf /root/ygkkkca\n")
	if err := os.WriteFile(scriptPath, legacyScript, 0o700); err != nil {
		t.Fatal(err)
	}
	certificatePath := filepath.Join(directory, "managed", "demo", "fullchain.pem")
	keyPath := filepath.Join(directory, "managed", "demo", "privkey.pem")
	if err := configureACMEOutputPaths(scriptPath, certificatePath, keyPath); err != nil {
		t.Fatal(err)
	}
	configured, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(configured), "/root/ygkkkca") {
		t.Fatalf("legacy path remains: %s", configured)
	}
	if !strings.Contains(string(configured), certificatePath) || !strings.Contains(string(configured), keyPath) {
		t.Fatalf("configured paths missing: %s", configured)
	}
	if _, err := os.Stat(filepath.Dir(certificatePath)); err != nil {
		t.Fatalf("certificate output directory was not created: %v", err)
	}
}

func TestConfigureACMEOutputPathsDisablesIPAndPortKillingModes(t *testing.T) {
	directory := t.TempDir()
	scriptPath := filepath.Join(directory, "acme.sh")
	upstreamFragment := `acme2(){
if [[ -n $(lsof -i :80|grep -v "PID") ]]; then
yellow "检测到80端口被占用，现执行80端口全释放"
sleep 2
lsof -i :80|grep -v "PID"|awk '{print "kill -9",$2}'|sh >/dev/null 2>&1
green "80端口全释放完毕！"
sleep 2
fi
}
ab="1.选择独立80端口模式申请IP证书（无需域名，小白推荐）\n2.选择独立80端口模式申请域名证书（需域名）\n3.选择DNS API模式申请证书（需域名、ID、Key），自动识别单域名与泛域名\n 请选择："
readp "$ab" cd
case "$cd" in
1 ) acme2 && acme3 && ACMEstandaloneIPcheck;;
2 ) acme2 && acme3 && ACMEstandaloneDNScheck;;
3 ) acme3 && ACMEDNScheck;;
esac
if [[ $domainIP = $v4 ]]; then
if [[ $domainIP = $v6 ]]; then
`
	if err := os.WriteFile(scriptPath, []byte(upstreamFragment), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := configureACMEOutputPaths(scriptPath, filepath.Join(directory, "cert.pem"), filepath.Join(directory, "key.pem")); err != nil {
		t.Fatal(err)
	}
	configured, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(configured)
	if strings.Contains(content, "kill -9") || strings.Contains(content, "申请IP证书") || strings.Contains(content, "ACMEstandaloneIPcheck") {
		t.Fatalf("unsafe upstream mode remains: %s", content)
	}
	if !strings.Contains(content, "VPNM 不会停止其他服务") || !strings.Contains(content, "$domainIP =~ $v4") {
		t.Fatalf("safe changes missing: %s", content)
	}
}

func testLocksWithACME(address, checksum string) dependency.Locks {
	asset := dependency.Asset{URL: "https://downloads.example.test/core", SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Archive: "zip", Member: "core"}
	assets := map[string]dependency.Asset{"amd64": asset, "arm64": asset}
	return dependency.Locks{SingBox: dependency.Core{Version: "1", Assets: assets}, Xray: dependency.Core{Version: "1", Assets: assets}, Realm: dependency.Core{Version: "1", Assets: assets}, ACME: dependency.Script{Commit: "0123456789abcdef0123456789abcdef01234567", URL: address, SHA256: checksum}}
}
