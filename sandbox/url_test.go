package sandbox

import (
	"context"
	"fmt"
	"testing"
)

type mockResolver struct {
	addrs map[string][]string
	err   error
}

func (m *mockResolver) LookupHost(_ context.Context, host string) ([]string, error) {
	if m.err != nil {
		return nil, m.err
	}
	if addrs, ok := m.addrs[host]; ok {
		return addrs, nil
	}
	return nil, fmt.Errorf("no such host: %s", host)
}

func TestValidateURL(t *testing.T) {
	origResolver := resolver
	defer func() { resolver = origResolver }()

	tests := []struct {
		name         string
		url          string
		allowPrivate bool
		addrs        map[string][]string
		resolveErr   error
		wantErr      bool
	}{
		{
			name:  "valid public URL",
			url:   "https://example.com/path",
			addrs: map[string][]string{"example.com": {"93.184.216.34"}},
		},
		{
			name:  "valid public URL with port",
			url:   "https://example.com:8443/path",
			addrs: map[string][]string{"example.com": {"93.184.216.34"}},
		},
		{
			name:    "empty URL",
			url:     "",
			wantErr: true,
		},
		{
			name:    "malformed URL",
			url:     "://bad",
			wantErr: true,
		},
		{
			name:    "ftp scheme blocked",
			url:     "ftp://example.com/file",
			wantErr: true,
		},
		{
			name:    "file scheme blocked",
			url:     "file:///etc/passwd",
			wantErr: true,
		},

		// Private networks
		{
			name:    "RFC1918 10.0.0.0/8",
			url:     "http://internal.corp",
			addrs:   map[string][]string{"internal.corp": {"10.0.0.1"}},
			wantErr: true,
		},
		{
			name:    "RFC1918 172.16.0.0/12",
			url:     "http://internal.corp",
			addrs:   map[string][]string{"internal.corp": {"172.16.5.1"}},
			wantErr: true,
		},
		{
			name:    "RFC1918 192.168.0.0/16",
			url:     "http://internal.corp",
			addrs:   map[string][]string{"internal.corp": {"192.168.1.1"}},
			wantErr: true,
		},

		// Loopback
		{
			name:    "loopback 127.0.0.1",
			url:     "http://localhost",
			addrs:   map[string][]string{"localhost": {"127.0.0.1"}},
			wantErr: true,
		},
		{
			name:    "loopback ::1",
			url:     "http://localhost",
			addrs:   map[string][]string{"localhost": {"::1"}},
			wantErr: true,
		},

		// Link-local
		{
			name:    "link-local 169.254.x.x",
			url:     "http://link-local.test",
			addrs:   map[string][]string{"link-local.test": {"169.254.1.1"}},
			wantErr: true,
		},

		// Cloud metadata
		{
			name:    "cloud metadata IP",
			url:     "http://metadata.test",
			addrs:   map[string][]string{"metadata.test": {"169.254.169.254"}},
			wantErr: true,
		},
		{
			name:    "metadata.google.internal hostname",
			url:     "http://metadata.google.internal/computeMetadata/v1/",
			wantErr: true,
		},
		{
			name:         "cloud metadata blocked even with allowPrivate",
			url:          "http://metadata.test",
			allowPrivate: true,
			addrs:        map[string][]string{"metadata.test": {"169.254.169.254"}},
			wantErr:      true,
		},

		// CGNAT
		{
			name:    "CGNAT 100.64.0.0/10",
			url:     "http://tailscale.test",
			addrs:   map[string][]string{"tailscale.test": {"100.100.1.1"}},
			wantErr: true,
		},

		// DNS failure (fail-closed)
		{
			name:       "DNS failure is blocked",
			url:        "http://unknown.test",
			resolveErr: fmt.Errorf("temporary DNS failure"),
			wantErr:    true,
		},

		// allowPrivate=true
		{
			name:         "allowPrivate allows RFC1918",
			url:          "http://internal.corp",
			allowPrivate: true,
			addrs:        map[string][]string{"internal.corp": {"10.0.0.1"}},
		},
		{
			name:         "allowPrivate allows loopback",
			url:          "http://localhost",
			allowPrivate: true,
			addrs:        map[string][]string{"localhost": {"127.0.0.1"}},
		},
		{
			name:         "allowPrivate allows CGNAT",
			url:          "http://tailscale.test",
			allowPrivate: true,
			addrs:        map[string][]string{"tailscale.test": {"100.100.1.1"}},
		},

		// Mixed addresses — one bad is enough
		{
			name:    "mixed public and private IPs",
			url:     "http://mixed.test",
			addrs:   map[string][]string{"mixed.test": {"93.184.216.34", "10.0.0.1"}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver = &mockResolver{addrs: tt.addrs, err: tt.resolveErr}
			err := ValidateURL(tt.url, tt.allowPrivate)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateURL(%q, %v) error = %v, wantErr %v", tt.url, tt.allowPrivate, err, tt.wantErr)
			}
		})
	}
}

func TestValidateRedirectURL(t *testing.T) {
	origResolver := resolver
	defer func() { resolver = origResolver }()

	resolver = &mockResolver{addrs: map[string][]string{"evil.test": {"10.0.0.1"}}}
	if err := ValidateRedirectURL("http://evil.test", false); err == nil {
		t.Error("ValidateRedirectURL should block private IPs on redirect")
	}
}
