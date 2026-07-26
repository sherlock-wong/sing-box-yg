package platform

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type responseTransport struct{ body string }

func (transport responseTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: io.NopCloser(strings.NewReader(transport.body)), Header: make(http.Header)}, nil
}

func TestAtomicWriteFileReplacesContentAndMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := AtomicWriteFile(path, []byte("new state"), 0o600); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil || string(contents) != "new state" {
		t.Fatalf("contents = %q, err = %v", contents, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, err = %v", info.Mode(), err)
	}
}

func TestRunRedactsOutputAndArguments(t *testing.T) {
	result, err := Run(context.Background(), Command{Path: "sh", Args: []string{"-c", "printf secret"}, Timeout: time.Second, Redact: []string{"secret"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Output != "[REDACTED]" {
		t.Fatalf("output = %q", result.Output)
	}
}

func TestDownloadVerifiedRejectsBadDigestWithoutInstalling(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vpnm")
	err := DownloadVerified(context.Background(), &http.Client{Transport: responseTransport{body: "payload"}}, Download{URL: "https://downloads.example.test/vpnm", SHA256: strings.Repeat("0", 64)}, path)
	if err == nil || !strings.Contains(err.Error(), "SHA-256 mismatch") {
		t.Fatalf("error = %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("destination exists: %v", err)
	}
}

func TestDownloadVerifiedInstallsMatchingPayload(t *testing.T) {
	payload := []byte("payload")
	digest := fmt.Sprintf("%x", sha256.Sum256(payload))
	path := filepath.Join(t.TempDir(), "vpnm")
	if err := DownloadVerified(context.Background(), &http.Client{Transport: responseTransport{body: string(payload)}}, Download{URL: "https://downloads.example.test/vpnm", SHA256: digest, Mode: 0o700}, path); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil || string(contents) != string(payload) {
		t.Fatalf("contents = %q, err = %v", contents, err)
	}
}

func TestAcquireLockBlocksUntilContextIsCanceled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.lock")
	first, err := AcquireLock(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Release()
	testContext, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := AcquireLock(testContext, path); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lock error = %v", err)
	}
	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
	second, err := AcquireLock(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := second.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestDownloadVerifiedRejectsRedirects(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusFound, Status: "302 Found", Header: http.Header{"Location": []string{"http://unsafe.example.test/vpnm"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
	})}
	err := DownloadVerified(context.Background(), client, Download{URL: "https://downloads.example.test/vpnm", SHA256: strings.Repeat("0", 64)}, filepath.Join(t.TempDir(), "vpnm"))
	if err == nil || !strings.Contains(err.Error(), "unexpected HTTP status") {
		t.Fatalf("error = %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
