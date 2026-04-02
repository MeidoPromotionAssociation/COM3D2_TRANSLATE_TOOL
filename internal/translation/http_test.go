package translation

import "testing"

func TestParseProxyAddressAddsHTTPFallbackScheme(t *testing.T) {
	parsed, err := parseProxyAddress("127.0.0.1:7890", "http")
	if err != nil {
		t.Fatalf("parseProxyAddress returned error: %v", err)
	}
	if parsed.Scheme != "http" {
		t.Fatalf("expected http scheme, got %q", parsed.Scheme)
	}
	if parsed.Host != "127.0.0.1:7890" {
		t.Fatalf("expected host 127.0.0.1:7890, got %q", parsed.Host)
	}
}

func TestParseProxyAddressPreservesExplicitScheme(t *testing.T) {
	parsed, err := parseProxyAddress("socks5://127.0.0.1:1080", "http")
	if err != nil {
		t.Fatalf("parseProxyAddress returned error: %v", err)
	}
	if parsed.Scheme != "socks5" {
		t.Fatalf("expected socks5 scheme, got %q", parsed.Scheme)
	}
	if parsed.Host != "127.0.0.1:1080" {
		t.Fatalf("expected host 127.0.0.1:1080, got %q", parsed.Host)
	}
}

func TestParseProxyAddressRejectsMissingHost(t *testing.T) {
	if _, err := parseProxyAddress("http://", "http"); err == nil {
		t.Fatal("expected error for missing host")
	}
}
