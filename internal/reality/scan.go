package reality

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"sort"
	"sync"
	"time"
)

// Result is intentionally JSON-friendly: sb.sh can consume this output while
// the Go manager is introduced incrementally.
type Result struct {
	Host        string `json:"host"`
	Successes   int    `json:"successes"`
	Samples     int    `json:"samples"`
	AverageMS   int64  `json:"average_ms"`
	JitterMS    int64  `json:"jitter_ms"`
	TLSVersion  string `json:"tls_version"`
	ALPN        string `json:"alpn"`
	Certificate string `json:"certificate"`
	Grade       int    `json:"grade"`
	Reason      string `json:"reason"`
}

type sample struct {
	latency time.Duration
	state   tls.ConnectionState
}

// Scan checks all targets concurrently. Each sample includes DNS resolution,
// TCP connect and a TLS 1.3 handshake from the current VPS.
func Scan(ctx context.Context, targets []string, samples, top int) ([]Result, error) {
	if samples < 1 {
		return nil, fmt.Errorf("samples must be positive")
	}
	if top < 0 {
		return nil, fmt.Errorf("top must not be negative")
	}

	results := make(chan Result, len(targets))
	var group sync.WaitGroup
	for _, target := range targets {
		target := target
		group.Add(1)
		go func() {
			defer group.Done()
			results <- scanTarget(ctx, target, samples)
		}()
	}
	group.Wait()
	close(results)

	ordered := make([]Result, 0, len(targets))
	for result := range results {
		ordered = append(ordered, result)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Grade != ordered[j].Grade {
			return ordered[i].Grade > ordered[j].Grade
		}
		if ordered[i].Successes != ordered[j].Successes {
			return ordered[i].Successes > ordered[j].Successes
		}
		if ordered[i].AverageMS != ordered[j].AverageMS {
			return ordered[i].AverageMS < ordered[j].AverageMS
		}
		return ordered[i].JitterMS < ordered[j].JitterMS
	})
	if top > 0 && len(ordered) > top {
		ordered = ordered[:top]
	}
	return ordered, nil
}

func scanTarget(ctx context.Context, host string, samples int) Result {
	result := Result{Host: host, Samples: samples}
	var total time.Duration
	var minimum, maximum time.Duration
	for attempt := 0; attempt < samples; attempt++ {
		measurement, err := probe(ctx, host, true)
		if err != nil {
			measurement, err = probe(ctx, host, false)
		}
		if err != nil {
			continue
		}
		if result.Successes == 0 {
			minimum, maximum = measurement.latency, measurement.latency
			result.TLSVersion = tlsVersion(measurement.state.Version)
			result.ALPN = measurement.state.NegotiatedProtocol
			result.Certificate = certificateName(measurement.state)
		}
		if measurement.latency < minimum {
			minimum = measurement.latency
		}
		if measurement.latency > maximum {
			maximum = measurement.latency
		}
		result.Successes++
		total += measurement.latency
	}
	if result.Successes < max(1, samples-1) {
		result.Reason = fmt.Sprintf("TLS 握手稳定性不足（%d/%d）", result.Successes, samples)
		return result
	}
	result.AverageMS = total.Milliseconds() / int64(result.Successes)
	result.JitterMS = (maximum - minimum).Milliseconds()
	if result.TLSVersion == "1.3" && result.ALPN == "h2" {
		// crypto/tls does not expose the negotiated key-exchange group. The
		// production shell scanner checks X25519 with OpenSSL before allowing
		// a target; this result is a preliminary recommendation only.
		result.Grade = 1
		result.Reason = "TLS 1.3 / h2 可用；使用前仍需检查 X25519"
	} else {
		result.Grade = 1
		result.Reason = "基础 TLS 可用，但未满足 TLS 1.3 / h2 推荐条件"
	}
	return result
}

func probe(ctx context.Context, host string, strict bool) (sample, error) {
	dialer := &net.Dialer{Timeout: 8 * time.Second}
	started := time.Now()
	raw, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, "443"))
	if err != nil {
		return sample{}, err
	}
	defer raw.Close()
	deadline := time.Now().Add(8 * time.Second)
	if contextDeadline, hasDeadline := ctx.Deadline(); hasDeadline && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	if err := raw.SetDeadline(deadline); err != nil {
		return sample{}, err
	}

	config := &tls.Config{
		ServerName: host,
		NextProtos: []string{"h2", "http/1.1"},
	}
	if strict {
		config.MinVersion = tls.VersionTLS13
		config.MaxVersion = tls.VersionTLS13
	}
	connection := tls.Client(raw, config)
	defer connection.Close()
	if err := connection.HandshakeContext(ctx); err != nil {
		return sample{}, err
	}
	state := connection.ConnectionState()
	return sample{latency: time.Since(started), state: state}, nil
}

func tlsVersion(version uint16) string {
	if version == tls.VersionTLS13 {
		return "1.3"
	}
	if version == tls.VersionTLS12 {
		return "1.2"
	}
	return "unknown"
}

func certificateName(state tls.ConnectionState) string {
	if len(state.PeerCertificates) == 0 {
		return ""
	}
	certificate := state.PeerCertificates[0]
	if certificate.Subject.CommonName != "" {
		return certificate.Subject.CommonName
	}
	if len(certificate.DNSNames) > 0 {
		return certificate.DNSNames[0]
	}
	return ""
}

func max(left, right int) int {
	if left > right {
		return left
	}
	return right
}
