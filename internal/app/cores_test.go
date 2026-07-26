package app

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/sherlock-wong/vps-net-manager/internal/dependency"
)

type archiveTransport map[string][]byte

func (transport archiveTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	contents := transport[request.URL.String()]
	return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: io.NopCloser(bytes.NewReader(contents)), Header: make(http.Header)}, nil
}

func TestCoreInstallerAtomicallyInstallsVerifiedLockedBinaries(t *testing.T) {
	singBox := tarGZ(t, "release/sing-box", []byte("sing-box-binary"))
	xray := zipArchive(t, "xray", []byte("xray-binary"))
	locks := dependency.Locks{SingBox: dependency.Core{Version: "1", Assets: map[string]dependency.Asset{}}, Xray: dependency.Core{Version: "2", Assets: map[string]dependency.Asset{}}, Realm: dependency.Core{Version: "3", Assets: map[string]dependency.Asset{}}}
	for _, architecture := range []string{"amd64", "arm64"} {
		locks.SingBox.Assets[architecture] = dependency.Asset{URL: "https://downloads.example.test/sing-box-" + architecture, SHA256: digest(singBox), Archive: "tar.gz", Member: "release/sing-box"}
		locks.Xray.Assets[architecture] = dependency.Asset{URL: "https://downloads.example.test/xray-" + architecture, SHA256: digest(xray), Archive: "zip", Member: "xray"}
		locks.Realm.Assets[architecture] = dependency.Asset{URL: "https://downloads.example.test/realm-" + architecture, SHA256: digest(singBox), Archive: "tar.gz", Member: "release/sing-box"}
	}
	root := t.TempDir()
	installer := CoreInstaller{StateDirectory: root, Locks: locks, Client: &http.Client{Transport: archiveTransport{"https://downloads.example.test/sing-box-amd64": singBox, "https://downloads.example.test/xray-amd64": xray}}}
	versions, err := installer.Install(context.Background(), "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if versions.SingBox != "1" || versions.Xray != "2" {
		t.Fatalf("versions = %+v", versions)
	}
	for _, expected := range []struct{ path, contents string }{{"sing-box", "sing-box-binary"}, {"xray", "xray-binary"}} {
		contents, err := os.ReadFile(filepath.Join(root, "bin", expected.path))
		if err != nil || string(contents) != expected.contents {
			t.Fatalf("%s = %q, %v", expected.path, contents, err)
		}
	}
	loaded, err := LoadInstalledCores(root)
	if err != nil || loaded != versions {
		t.Fatalf("loaded=%+v err=%v", loaded, err)
	}
}

func tarGZ(t *testing.T, name string, contents []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	writer := tar.NewWriter(gzipWriter)
	if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(contents))}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(contents); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
func zipArchive(t *testing.T, name string, contents []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	entry, err := writer.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write(contents); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
func digest(contents []byte) string {
	value := sha256.Sum256(contents)
	return hex.EncodeToString(value[:])
}
