package app

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/sherlock-wong/vps-net-manager/internal/model"
)

type updateTransport map[string]string

func (transport updateTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	contents, found := transport[request.URL.String()]
	if !found {
		return &http.Response{StatusCode: http.StatusNotFound, Status: "404 Not Found", Body: io.NopCloser(bytes.NewReader(nil)), Header: make(http.Header)}, nil
	}
	return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: io.NopCloser(bytes.NewBufferString(contents)), Header: make(http.Header)}, nil
}

func TestParseChecksumsRejectsDuplicateAndUnsafeNames(t *testing.T) {
	digest := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	for _, contents := range []string{digest + "  vpnm-linux-amd64\n" + digest + "  vpnm-linux-amd64\n", digest + "  ../vpnm\n"} {
		if _, err := parseChecksums(contents); err == nil {
			t.Fatalf("parseChecksums accepted %q", contents)
		}
	}
}

func TestUpdaterReportsCurrentSuccessfulBuildWithoutDownloadingBinaries(t *testing.T) {
	commit := "0123456789abcdef0123456789abcdef01234567"
	manifest := `{"source_commit":"` + commit + `","built_at":"2026-07-27T00:00:00Z","binaries":{},"dependencies":{}}`
	client := &http.Client{Transport: updateTransport{
		"https://api.github.com/repos/example/vpnm/git/ref/heads/main-build":          `{"object":{"sha":"` + commit + `"}}`,
		"https://raw.githubusercontent.com/example/vpnm/" + commit + "/manifest.json": manifest,
		"https://raw.githubusercontent.com/example/vpnm/" + commit + "/checksums.txt": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa  vpnm-linux-amd64\n",
	}}
	result, err := (Updater{Repository: "example/vpnm", StateDirectory: t.TempDir(), BinaryPath: "/tmp/vpnm", Client: client, Architecture: "amd64"}).Update(context.Background(), commit, model.NewState())
	if err != nil {
		t.Fatal(err)
	}
	if result.Updated || result.SourceCommit != commit {
		t.Fatalf("result = %+v", result)
	}
}
