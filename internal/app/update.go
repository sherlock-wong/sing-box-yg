package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/sherlock-wong/vps-net-manager/internal/dependency"
	"github.com/sherlock-wong/vps-net-manager/internal/model"
	"github.com/sherlock-wong/vps-net-manager/internal/platform"
)

const DefaultRepository = "sherlock-wong/vps-net-manager"

var commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

type BuildManifest struct {
	SourceCommit string                 `json:"source_commit"`
	BuiltAt      time.Time              `json:"built_at"`
	Binaries     map[string]BuildBinary `json:"binaries"`
	Dependencies dependency.Locks       `json:"dependencies"`
}

type BuildBinary struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

type Updater struct {
	Repository     string
	StateDirectory string
	BinaryPath     string
	Client         *http.Client
	Runner         platform.CommandRunner
	Architecture   string
}

type UpdateResult struct {
	SourceCommit string
	Updated      bool
	Cores        InstalledCores
}

func (updater Updater) Update(ctx context.Context, currentCommit string, state model.State) (UpdateResult, error) {
	repository := updater.Repository
	if repository == "" {
		repository = DefaultRepository
	}
	architecture := updater.Architecture
	if architecture == "" {
		architecture = runtime.GOARCH
	}
	if architecture != "amd64" && architecture != "arm64" {
		return UpdateResult{}, fmt.Errorf("unsupported architecture %q", architecture)
	}
	if updater.StateDirectory == "" || updater.BinaryPath == "" {
		return UpdateResult{}, fmt.Errorf("state directory and binary path are required")
	}
	commit, manifest, checksums, err := updater.fetchBuild(ctx, repository)
	if err != nil {
		return UpdateResult{}, err
	}
	if manifest.SourceCommit == currentCommit {
		return UpdateResult{SourceCommit: manifest.SourceCommit}, nil
	}
	binary, found := manifest.Binaries[architecture]
	if !found || binary.Name != "vpnm-linux-"+architecture || !validSHA256(binary.SHA256) {
		return UpdateResult{}, fmt.Errorf("manifest lacks a valid %s vpnm binary", architecture)
	}
	if checksums[binary.Name] != binary.SHA256 {
		return UpdateResult{}, fmt.Errorf("manifest and checksums disagree for %s", binary.Name)
	}
	if err := manifest.Dependencies.Validate(); err != nil {
		return UpdateResult{}, fmt.Errorf("validate remote dependency locks: %w", err)
	}
	stage, err := os.MkdirTemp("", "vpnm-update-")
	if err != nil {
		return UpdateResult{}, fmt.Errorf("create update staging directory: %w", err)
	}
	defer os.RemoveAll(stage)
	base := "https://raw.githubusercontent.com/" + repository + "/" + commit
	stagedVPNM := filepath.Join(stage, "vpnm")
	if err := platform.DownloadVerified(ctx, updater.Client, platform.Download{URL: base + "/" + binary.Name, SHA256: binary.SHA256, Mode: 0o755}, stagedVPNM); err != nil {
		return UpdateResult{}, fmt.Errorf("download new vpnm: %w", err)
	}
	if err := updater.checkNewManager(ctx, stagedVPNM); err != nil {
		return UpdateResult{}, err
	}
	coreArtifacts, cores, err := (CoreInstaller{StateDirectory: updater.StateDirectory, Locks: manifest.Dependencies, Client: updater.Client}).Stage(ctx, architecture)
	if err != nil {
		return UpdateResult{}, fmt.Errorf("stage new cores: %w", err)
	}
	if err := updater.checkStagedCores(ctx, stage, coreArtifacts, state); err != nil {
		return UpdateResult{}, err
	}
	contents, err := os.ReadFile(stagedVPNM)
	if err != nil {
		return UpdateResult{}, fmt.Errorf("read staged vpnm: %w", err)
	}
	artifacts := append([]Artifact{{Path: updater.BinaryPath, Contents: contents, Mode: 0o755}}, coreArtifacts...)
	rollback, err := (FilesystemStore{}).Commit(ctx, artifacts)
	if err != nil {
		return UpdateResult{}, fmt.Errorf("install update artifacts: %w", err)
	}
	applyOptions := DefaultApplyOptions(updater.StateDirectory, &state)
	if _, err := applyOptions.Apply(ctx, state); err != nil {
		rollbackErr := rollback(ctx)
		_, restoreErr := DefaultApplyOptions(updater.StateDirectory, &state).Apply(ctx, state)
		return UpdateResult{}, joinFailure(fmt.Errorf("apply updated services: %w", err), rollbackErr, restoreErr)
	}
	return UpdateResult{SourceCommit: manifest.SourceCommit, Updated: true, Cores: cores}, nil
}

func (updater Updater) checkNewManager(ctx context.Context, binary string) error {
	runner := updater.Runner
	if runner == nil {
		runner = platform.SystemRunner{}
	}
	_, err := runner.Run(ctx, platform.Command{Path: binary, Args: []string{"state", "validate", "--state", filepath.Join(updater.StateDirectory, "state.json")}, Timeout: 30 * time.Second})
	if err != nil {
		return fmt.Errorf("new vpnm state preflight: %w", err)
	}
	return nil
}

func (updater Updater) checkStagedCores(ctx context.Context, stage string, artifacts []Artifact, state model.State) error {
	paths := make(map[string]string, len(artifacts))
	for _, artifact := range artifacts {
		name := filepath.Base(artifact.Path)
		if name != "sing-box" && name != "xray" {
			continue
		}
		path := filepath.Join(stage, name)
		if err := platform.AtomicWriteFile(path, artifact.Contents, 0o755); err != nil {
			return err
		}
		paths[name] = path
	}
	rendered, err := RenderState(state)
	if err != nil {
		return err
	}
	return (CommandCoreChecker{SingBoxPath: paths["sing-box"], XrayPath: paths["xray"]}).CheckCores(ctx, rendered.Server)
}

func (updater Updater) fetchBuild(ctx context.Context, repository string) (string, BuildManifest, map[string]string, error) {
	ref, err := updater.get(ctx, "https://api.github.com/repos/"+repository+"/git/ref/heads/main-build")
	if err != nil {
		return "", BuildManifest{}, nil, fmt.Errorf("resolve main-build: %w", err)
	}
	var parsed struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := json.Unmarshal(ref, &parsed); err != nil || !commitPattern.MatchString(parsed.Object.SHA) {
		return "", BuildManifest{}, nil, fmt.Errorf("main-build returned no immutable commit")
	}
	commit := parsed.Object.SHA
	base := "https://raw.githubusercontent.com/" + repository + "/" + commit
	manifestBytes, err := updater.get(ctx, base+"/manifest.json")
	if err != nil {
		return "", BuildManifest{}, nil, fmt.Errorf("download manifest: %w", err)
	}
	var manifest BuildManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return "", BuildManifest{}, nil, fmt.Errorf("parse manifest: %w", err)
	}
	if !commitPattern.MatchString(manifest.SourceCommit) {
		return "", BuildManifest{}, nil, fmt.Errorf("manifest source commit is invalid")
	}
	checksumBytes, err := updater.get(ctx, base+"/checksums.txt")
	if err != nil {
		return "", BuildManifest{}, nil, fmt.Errorf("download checksums: %w", err)
	}
	checksums, err := parseChecksums(string(checksumBytes))
	if err != nil {
		return "", BuildManifest{}, nil, err
	}
	return commit, manifest, checksums, nil
}

func (updater Updater) get(ctx context.Context, address string) ([]byte, error) {
	client := updater.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	local := *client
	local.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	response, err := local.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected HTTP status %s", response.Status)
	}
	return io.ReadAll(io.LimitReader(response.Body, 5<<20))
}

func parseChecksums(contents string) (map[string]string, error) {
	checksums := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(contents), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || !validSHA256(fields[0]) || strings.Contains(fields[1], "/") || fields[1] == "" {
			return nil, fmt.Errorf("invalid checksums.txt")
		}
		if _, exists := checksums[fields[1]]; exists {
			return nil, fmt.Errorf("duplicate checksum for %s", fields[1])
		}
		checksums[fields[1]] = strings.ToLower(fields[0])
	}
	return checksums, nil
}

func validSHA256(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}
