//go:build windows

package translation

import "testing"

func TestResolveWindowsProxyServerUsesHTTPSchemeForHTTPSTargets(t *testing.T) {
	parsed, ok, err := resolveWindowsProxyServer("127.0.0.1:7890", "https")
	if err != nil {
		t.Fatalf("resolveWindowsProxyServer returned error: %v", err)
	}
	if !ok {
		t.Fatal("expected proxy to be resolved")
	}
	if parsed.Scheme != "http" {
		t.Fatalf("expected http proxy scheme for https target, got %q", parsed.Scheme)
	}
	if parsed.Host != "127.0.0.1:7890" {
		t.Fatalf("expected host 127.0.0.1:7890, got %q", parsed.Host)
	}
}

func TestResolveWindowsProxyServerSupportsSOCKSProxyEntries(t *testing.T) {
	parsed, ok, err := resolveWindowsProxyServer("http=127.0.0.1:7890;https=127.0.0.1:7890;socks=127.0.0.1:1080", "ftp")
	if err != nil {
		t.Fatalf("resolveWindowsProxyServer returned error: %v", err)
	}
	if !ok {
		t.Fatal("expected proxy to be resolved")
	}
	if parsed.Scheme != "socks5" {
		t.Fatalf("expected socks5 proxy scheme, got %q", parsed.Scheme)
	}
	if parsed.Host != "127.0.0.1:1080" {
		t.Fatalf("expected host 127.0.0.1:1080, got %q", parsed.Host)
	}
}
