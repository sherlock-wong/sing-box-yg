package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Download struct {
	URL    string
	SHA256 string
	Mode   os.FileMode
}

// DownloadVerified only accepts HTTPS and installs a verified, atomically
// written file. Callers own the destination path and temporary directory.
func DownloadVerified(ctx context.Context, client *http.Client, request Download, destination string) error {
	parsed, err := url.Parse(request.URL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return fmt.Errorf("download URL must be HTTPS")
	}
	expected, err := hex.DecodeString(request.SHA256)
	if err != nil || len(expected) != sha256.Size {
		return fmt.Errorf("SHA-256 must be a 64-character hexadecimal digest")
	}
	if client == nil {
		client = &http.Client{Timeout: 45 * time.Second}
	}
	// GitHub release assets use signed HTTPS redirects. Follow only a bounded
	// HTTPS-only chain so verified dependencies cannot be downgraded to HTTP.
	localClient := *client
	localClient.CheckRedirect = func(next *http.Request, previous []*http.Request) error {
		if next.URL.Scheme != "https" || next.URL.Host == "" {
			return fmt.Errorf("redirect URL must be HTTPS")
		}
		if len(previous) >= 5 {
			return fmt.Errorf("too many download redirects")
		}
		return nil
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, request.URL, nil)
	if err != nil {
		return fmt.Errorf("create download request: %w", err)
	}
	response, err := localClient.Do(httpRequest)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download: unexpected HTTP status %s", response.Status)
	}
	temporaryDirectory, err := os.MkdirTemp(filepath.Dir(destination), ".vpnm-download-")
	if err != nil {
		return fmt.Errorf("create download directory: %w", err)
	}
	defer os.RemoveAll(temporaryDirectory)
	temporaryPath := filepath.Join(temporaryDirectory, "payload")
	temporary, err := os.OpenFile(temporaryPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create download file: %w", err)
	}
	hash := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(temporary, hash), response.Body)
	closeErr := temporary.Close()
	if copyErr != nil {
		return fmt.Errorf("save download: %w", copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close download: %w", closeErr)
	}
	if !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), request.SHA256) {
		return fmt.Errorf("download SHA-256 mismatch")
	}
	contents, err := os.ReadFile(temporaryPath)
	if err != nil {
		return fmt.Errorf("read verified download: %w", err)
	}
	mode := request.Mode
	if mode == 0 {
		mode = 0o700
	}
	return AtomicWriteFile(destination, contents, mode)
}
