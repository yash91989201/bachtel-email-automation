package ai

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"
)

// apiRoot normalizes and checks one connection's configured root without a
// protocol path. Production policy is encrypted transport only; the test-only
// private allowance also permits plaintext so simulated servers work without
// weakening the shipped configuration.
func apiRoot(cfg Config, allowPrivate bool) (string, error) {
	root := strings.TrimSpace(cfg.APIRoot)
	if root == "" || len(root) > 2048 {
		return "", ErrEndpointUnsafe
	}
	parsed, err := url.Parse(root)
	if err != nil {
		return "", ErrEndpointUnsafe
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Opaque != "" {
		return "", ErrEndpointUnsafe
	}
	if allowPrivate {
		if parsed.Scheme != "https" && parsed.Scheme != "http" {
			return "", ErrEndpointUnsafe
		}
	} else if parsed.Scheme != "https" {
		return "", ErrEndpointUnsafe
	}
	if parsed.Host == "" {
		return "", ErrEndpointUnsafe
	}
	if _, err := checkHost(parsed.Hostname(), allowPrivate); err != nil {
		return "", err
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

// endpointURL returns the validated request URL of one API format.
func endpointURL(cfg Config, allowPrivate bool) (string, error) {
	base, err := apiRoot(cfg, allowPrivate)
	if err != nil {
		return "", err
	}
	return base + cfg.APIFormat.Path(), nil
}

// checkHost refuses literal private targets before any connection is made. A
// hostname is resolved by the dialer, whose control hook repeats the check on
// the resolved address so a name cannot point at a private service.
func checkHost(host string, allowPrivate bool) (string, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return "", ErrEndpointUnsafe
	}
	if ip := net.ParseIP(host); ip != nil {
		if !allowPrivate && !publicAddress(ip) {
			return "", ErrEndpointUnsafe
		}
		return ip.String(), nil
	}
	return host, nil
}

// publicAddress reports an address that is safe to reach from a deployment:
// globally routable unicast only. Loopback, private, link-local, metadata,
// multicast and unspecified targets are refused.
func publicAddress(ip net.IP) bool {
	if ip.IsUnspecified() || ip.IsLoopback() || ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
		return false
	}
	// The cloud metadata endpoint is link-local and covered above; the check
	// stays explicit so a future exemption cannot quietly re-enable it.
	if ip.Equal(net.ParseIP("169.254.169.254")) {
		return false
	}
	if ip4 := ip.To4(); ip4 != nil && ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127 {
		// Shared address space (RFC 6598) is not a public destination.
		return false
	}
	return ip.IsGlobalUnicast()
}

// dialControl checks every connection destination, including a redirect's, on
// the resolved IP address.
func dialControl(allowPrivate bool) func(network, address string, _ syscall.RawConn) error {
	return func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return ErrEndpointUnsafe
		}
		ip := net.ParseIP(host)
		if ip == nil {
			return ErrEndpointUnsafe
		}
		if !allowPrivate && !publicAddress(ip) {
			return ErrEndpointUnsafe
		}
		return nil
	}
}

// newClient builds a bounded client that never forwards credentials to a
// different origin. Redirects must stay on the exact validated origin.
func newClient(o ValidationOptions) *http.Client {
	if o.Timeout <= 0 {
		o.Timeout = DefaultValidationOptions().Timeout
	}
	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
		Control: dialControl(o.AllowPrivate),
	}
	transport := &http.Transport{
		// No environment proxy: a proxy could receive the API key or reach a
		// private service the endpoint policy refuses.
		Proxy:                 nil,
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: o.Timeout,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          1,
		IdleConnTimeout:       time.Minute,
	}
	return &http.Client{
		Timeout:   o.Timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return ErrEndpointUnsafe
			}
			if !sameOrigin(via[0].URL, req.URL) {
				return ErrEndpointUnsafe
			}
			if req.URL.Scheme != "https" && !o.AllowPrivate {
				return ErrEndpointUnsafe
			}
			return nil
		},
	}
}

func sameOrigin(first, next *url.URL) bool {
	return first.Scheme == next.Scheme && strings.EqualFold(first.Host, next.Host)
}

// classifyTransport maps a request error to a fixed operator-safe failure.
// Cancellation and deadline exhaustion share one outcome so an aborted run
// never reads as a provider outage.
func classifyTransport(err error) error {
	if errors.Is(err, ErrEndpointUnsafe) {
		return ErrEndpointUnsafe
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return ErrEndpointTimeout
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return ErrEndpointTimeout
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Timeout() {
		return ErrEndpointTimeout
	}
	return fmt.Errorf("%w: %v", ErrProviderFailure, "the endpoint could not be reached")
}
