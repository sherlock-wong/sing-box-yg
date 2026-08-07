// Package dependency owns the embedded, machine-readable core lock manifest.
package dependency

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"regexp"

	"github.com/sherlock-wong/vps-net-manager/assets"
)

type Locks struct {
	SingBox        Core   `json:"sing_box"`
	Xray           Core   `json:"xray"`
	Realm          Core   `json:"realm"`
	Komari         Core   `json:"komari"`
	ACME           Script `json:"acme"`
	ACMECloudflare Script `json:"acme_cloudflare"`
}
type Script struct {
	Commit string `json:"commit"`
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}
type Core struct {
	Version string           `json:"version"`
	Assets  map[string]Asset `json:"assets"`
}
type Asset struct {
	URL     string `json:"url"`
	SHA256  string `json:"sha256"`
	Archive string `json:"archive"`
	Member  string `json:"member"`
}

func Embedded() (Locks, error) {
	var locks Locks
	if err := json.Unmarshal(assets.DependencyLocks, &locks); err != nil {
		return Locks{}, fmt.Errorf("parse embedded dependency locks: %w", err)
	}
	if err := locks.Validate(); err != nil {
		return Locks{}, err
	}
	return locks, nil
}

func (locks Locks) Validate() error {
	for _, item := range []struct {
		name string
		core Core
	}{{"sing-box", locks.SingBox}, {"xray", locks.Xray}, {"realm", locks.Realm}, {"komari", locks.Komari}} {
		if item.core.Version == "" {
			return fmt.Errorf("%s lock has no version", item.name)
		}
		for _, architecture := range []string{"amd64", "arm64"} {
			asset, found := item.core.Assets[architecture]
			if !found {
				return fmt.Errorf("%s lock lacks %s", item.name, architecture)
			}
			if err := asset.validate(); err != nil {
				return fmt.Errorf("%s %s: %w", item.name, architecture, err)
			}
		}
	}
	if locks.ACME.Commit != "" {
		if !commitPattern.MatchString(locks.ACME.Commit) {
			return fmt.Errorf("acme lock has invalid commit")
		}
		if err := locks.ACME.validate(); err != nil {
			return fmt.Errorf("acme: %w", err)
		}
	}
	if locks.ACMECloudflare.Commit != "" {
		if !commitPattern.MatchString(locks.ACMECloudflare.Commit) {
			return fmt.Errorf("acme Cloudflare lock has invalid commit")
		}
		if err := locks.ACMECloudflare.validate(); err != nil {
			return fmt.Errorf("acme Cloudflare: %w", err)
		}
	}
	return nil
}

func (locks Locks) Asset(name, architecture string) (Core, Asset, error) {
	var core Core
	switch name {
	case "sing-box":
		core = locks.SingBox
	case "xray":
		core = locks.Xray
	case "realm":
		core = locks.Realm
	case "komari":
		core = locks.Komari
	default:
		return Core{}, Asset{}, fmt.Errorf("unknown core %q", name)
	}
	asset, found := core.Assets[architecture]
	if !found {
		return Core{}, Asset{}, fmt.Errorf("%s does not support %s", name, architecture)
	}
	return core, asset, nil
}

func (asset Asset) validate() error {
	parsed, err := url.Parse(asset.URL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return fmt.Errorf("URL must be HTTPS")
	}
	digest, err := hex.DecodeString(asset.SHA256)
	if err != nil || len(digest) != sha256.Size {
		return fmt.Errorf("SHA-256 must be 64 hexadecimal characters")
	}
	if asset.Archive != "tar.gz" && asset.Archive != "zip" && asset.Archive != "raw" {
		return fmt.Errorf("unsupported archive %q", asset.Archive)
	}
	if asset.Archive == "raw" {
		if asset.Member != "" {
			return fmt.Errorf("raw asset must not specify an archive member")
		}
		return nil
	}
	if asset.Member == "" || path.IsAbs(asset.Member) || path.Clean(asset.Member) != asset.Member || path.Clean(asset.Member) == "." {
		return fmt.Errorf("unsafe archive member")
	}
	return nil
}

var commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

func (script Script) validate() error {
	parsed, err := url.Parse(script.URL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return fmt.Errorf("URL must be HTTPS")
	}
	digest, err := hex.DecodeString(script.SHA256)
	if err != nil || len(digest) != sha256.Size {
		return fmt.Errorf("SHA-256 must be 64 hexadecimal characters")
	}
	return nil
}
