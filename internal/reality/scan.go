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
	if top < 1 {
		return nil, fmt.Errorf("top must be positive")
	}

	results := make(chan Result, len(targets))
	var group sync.WaitGroup
	for _, target := range targets {
		target := target
		group.Add(1)
		go func() {
			defer group.Done()
			if result, ok := scanTarget(ctx, target, samples); ok {
				results <- result
			}
		}()
	}
	group.Wait()
	close(results)

	ordered := make([]Result, 0, len(targets))
	for result := range results {
		ordered = append(ordered, result)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Successes != ordered[j].Successes {
			return ordered[i].Successes > ordered[j].Successes
		}
		if ordered[i].AverageMS != ordered[j].AverageMS {
			return ordered[i].AverageMS < ordered[j].AverageMS
		}
		return ordered[i].JitterMS < ordered[j].JitterMS
	})
	if len(ordered) > top {
		ordered = ordered[:top]
	}
	return ordered, nil
}

func scanTarget(ctx context.Context, host string, samples int) (Result, bool) {
	result := Result{Host: host, Samples: samples}
	var total time.Duration
	var minimum, maximum time.Duration
	for attempt := 0; attempt < samples; attempt++ {
		measurement, err := probe(ctx, host)
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
		return Result{}, false
	}
	result.AverageMS = total.Milliseconds() / int64(result.Successes)
	result.JitterMS = (maximum - minimum).Milliseconds()
	return result, true
}

func probe(ctx context.Context, host string) (sample, error) {
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

	connection := tls.Client(raw, &tls.Config{
		ServerName: host,
		MinVersion: tls.VersionTLS13,
		NextProtos: []string{"h2", "http/1.1"},
	})
	defer connection.Close()
	if err := connection.HandshakeContext(ctx); err != nil {
		return sample{}, err
	}
	state := connection.ConnectionState()
	if state.Version != tls.VersionTLS13 || state.NegotiatedProtocol != "h2" {
		return sample{}, fmt.Errorf("target lacks required TLS 1.3 / HTTP2 profile")
	}
	return sample{latency: time.Since(started), state: state}, nil
}

func tlsVersion(version uint16) string {
	if version == tls.VersionTLS13 {
		return "1.3"
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
