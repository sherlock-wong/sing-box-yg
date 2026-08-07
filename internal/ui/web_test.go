package ui

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sherlock-wong/vps-net-manager/internal/certificate"
	"github.com/sherlock-wong/vps-net-manager/internal/komari"
	"github.com/sherlock-wong/vps-net-manager/internal/model"
	"github.com/sherlock-wong/vps-net-manager/internal/web"
)

func webTestState(t *testing.T, domain string) model.State {
	t.Helper()
	directory := t.TempDir()
	certPEM, keyPEM, _, err := certificate.CreatePinned(domain, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	certPath, keyPath := filepath.Join(directory, "cert.pem"), filepath.Join(directory, "key.pem")
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	state := model.NewState()
	state.Certificates["test-cert"] = model.Certificate{Name: "test", Cert: certPath, Key: keyPath, Mode: model.CertificateModePinned}
	return state
}

func TestEditWebAddsProxyWithMatchingCertificate(t *testing.T) {
	state := webTestState(t, "monitor.example.com")
	input := bufio.NewScanner(strings.NewReader("2\nmonitor.example.com\n49501\n\n25774\n5\n"))
	var output strings.Builder
	deployment, candidate, changed, err := EditWeb(context.Background(), input, &output, t.TempDir(), state, web.NewState())
	if err != nil {
		t.Fatal(err)
	}
	if !changed || deployment.Certificates["test-cert"].Name != "test" || len(candidate.Proxies) != 1 {
		t.Fatalf("changed=%v deployment=%+v candidate=%+v", changed, deployment, candidate)
	}
	proxy := candidate.Proxies[0]
	if proxy.Domain != "monitor.example.com" || proxy.ListenPort != 49501 || proxy.TargetHost != "127.0.0.1" || proxy.TargetPort != 25774 || proxy.CertificateID != "test-cert" {
		t.Fatalf("proxy=%+v", proxy)
	}
	if !strings.Contains(output.String(), "已找到覆盖 monitor.example.com 的证书") {
		t.Fatalf("missing certificate match output: %s", output.String())
	}
}

func TestEditKomariDomainCreatesLinkedProxy(t *testing.T) {
	state := webTestState(t, "status.example.com")
	input := bufio.NewScanner(strings.NewReader("2\n2\nstatus.example.com\n49502\n49503\n5\n"))
	var output strings.Builder
	deployment, candidateWeb, candidateKomari, changed, updateRequested, err := EditKomari(context.Background(), input, &output, t.TempDir(), state, web.NewState(), komari.NewState(), true)
	if err != nil {
		t.Fatal(err)
	}
	if updateRequested || !changed || deployment.Certificates["test-cert"].Name != "test" || candidateKomari.Mode != komari.ModeDomain || candidateKomari.ListenHost != "127.0.0.1" || len(candidateWeb.Proxies) != 1 {
		t.Fatalf("changed=%v komari=%+v web=%+v", changed, candidateKomari, candidateWeb)
	}
	proxy := candidateWeb.Proxies[0]
	if proxy.ID != candidateKomari.ProxyID || proxy.TargetHost != "127.0.0.1" || proxy.TargetPort != 49503 || proxy.ListenPort != 49502 {
		t.Fatalf("linked proxy=%+v komari=%+v", proxy, candidateKomari)
	}
}

func TestEditKomariRequestsLockedBinaryUpdate(t *testing.T) {
	input := bufio.NewScanner(strings.NewReader("4\n"))
	var output strings.Builder
	_, _, _, changed, updateRequested, err := EditKomari(context.Background(), input, &output, t.TempDir(), model.NewState(), web.NewState(), komari.NewState(), true)
	if err != nil {
		t.Fatal(err)
	}
	if changed || !updateRequested {
		t.Fatalf("changed=%v updateRequested=%v", changed, updateRequested)
	}
}

func TestEditKomariRejectsDomainModeBeforeDomainPromptWhenNginxMissing(t *testing.T) {
	input := bufio.NewScanner(strings.NewReader("2\n2\n0\n"))
	var output strings.Builder
	_, _, _, changed, updateRequested, err := EditKomari(context.Background(), input, &output, t.TempDir(), model.NewState(), web.NewState(), komari.NewState(), false)
	if err != nil {
		t.Fatal(err)
	}
	if changed || updateRequested || !strings.Contains(output.String(), "未安装 Nginx") || strings.Contains(output.String(), "访问域名") {
		t.Fatalf("changed=%v update=%v output=%s", changed, updateRequested, output.String())
	}
}
