package sandbox

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

// Resolver abstracts DNS resolution for testing.
type Resolver interface {
	LookupHost(ctx context.Context, host string) ([]string, error)
}

var resolver Resolver = net.DefaultResolver

var blockedCIDRs []*net.IPNet

func init() {
	for _, cidr := range []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
		"169.254.0.0/16",
		"100.64.0.0/10",
		"::1/128",
		"fe80::/10",
	} {
		_, n, _ := net.ParseCIDR(cidr)
		blockedCIDRs = append(blockedCIDRs, n)
	}
}

var blockedHostnames = []string{
	"metadata.google.internal",
}

// metadataIP is the cloud metadata endpoint, blocked even when allowPrivate is true.
var metadataIP = net.ParseIP("169.254.169.254")

// ValidateURL checks that rawURL is safe to fetch. It resolves DNS before
// checking IPs to prevent DNS rebinding. Fails closed on DNS errors.
func ValidateURL(rawURL string, allowPrivate bool) error {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return fmt.Errorf("invalid URL: %q", rawURL)
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("blocked scheme %q: only http and https are allowed", u.Scheme)
	}

	host := u.Hostname()

	for _, blocked := range blockedHostnames {
		if strings.EqualFold(host, blocked) {
			return fmt.Errorf("blocked hostname: %s", host)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	addrs, err := resolver.LookupHost(ctx, host)
	if err != nil {
		return fmt.Errorf("DNS resolution failed (fail-closed): %w", err)
	}
	if len(addrs) == 0 {
		return fmt.Errorf("DNS resolution returned no addresses for %s", host)
	}

	for _, addr := range addrs {
		ip := net.ParseIP(addr)
		if ip == nil {
			return fmt.Errorf("unparseable resolved IP %q for %s", addr, host)
		}

		if ip.Equal(metadataIP) {
			return fmt.Errorf("blocked cloud metadata endpoint: %s", addr)
		}

		if !allowPrivate {
			for _, cidr := range blockedCIDRs {
				if cidr.Contains(ip) {
					return fmt.Errorf("blocked private/reserved IP %s (in %s)", addr, cidr)
				}
			}
		}
	}

	return nil
}

// ValidateRedirectURL validates a URL encountered during a redirect chain.
// Same rules as ValidateURL — re-validates at each hop.
func ValidateRedirectURL(rawURL string, allowPrivate bool) error {
	return ValidateURL(rawURL, allowPrivate)
}
