//go:build windows

package translation

import (
	"net/http"
	"net/url"
	"path"
	"strings"

	"golang.org/x/sys/windows/registry"
)

func systemProxyFunc() func(*http.Request) (*url.URL, error) {
	return func(req *http.Request) (*url.URL, error) {
		if req == nil || req.URL == nil {
			return nil, nil
		}

		if proxyURL, ok, err := windowsInternetSettingsProxy(req.URL); ok || err != nil {
			return proxyURL, err
		}
		return http.ProxyFromEnvironment(req)
	}
}

func windowsInternetSettingsProxy(target *url.URL) (*url.URL, bool, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Internet Settings`, registry.QUERY_VALUE)
	if err != nil {
		return nil, false, nil
	}
	defer key.Close()

	enabled, _, err := key.GetIntegerValue("ProxyEnable")
	if err != nil || enabled == 0 {
		return nil, false, nil
	}

	if override, _, err := key.GetStringValue("ProxyOverride"); err == nil && shouldBypassWindowsProxy(override, target.Hostname()) {
		return nil, true, nil
	}

	proxyServer, _, err := key.GetStringValue("ProxyServer")
	if err != nil || strings.TrimSpace(proxyServer) == "" {
		return nil, false, nil
	}

	proxyURL, ok, err := resolveWindowsProxyServer(proxyServer, target.Scheme)
	if err != nil {
		return nil, true, err
	}
	if !ok {
		return nil, false, nil
	}
	return proxyURL, true, nil
}

func shouldBypassWindowsProxy(rawOverride string, host string) bool {
	host = strings.TrimSpace(strings.ToLower(host))
	if host == "" {
		return false
	}

	for _, token := range strings.Split(rawOverride, ";") {
		pattern := strings.TrimSpace(strings.ToLower(token))
		if pattern == "" {
			continue
		}
		if pattern == "<local>" {
			if !strings.Contains(host, ".") {
				return true
			}
			continue
		}
		if matched, err := path.Match(pattern, host); err == nil && matched {
			return true
		}
	}
	return false
}

func resolveWindowsProxyServer(rawServer string, scheme string) (*url.URL, bool, error) {
	server := strings.TrimSpace(rawServer)
	if server == "" {
		return nil, false, nil
	}

	if strings.Contains(server, "=") {
		for _, part := range strings.Split(server, ";") {
			name, value, ok := strings.Cut(part, "=")
			if !ok {
				continue
			}
			name = strings.TrimSpace(strings.ToLower(name))
			if name != strings.ToLower(scheme) {
				continue
			}
			return parseWindowsProxyValue(name, value)
		}
		if strings.EqualFold(scheme, "https") {
			for _, part := range strings.Split(server, ";") {
				name, value, ok := strings.Cut(part, "=")
				if ok && strings.TrimSpace(strings.ToLower(name)) == "http" {
					return parseWindowsProxyValue("http", value)
				}
			}
		}
		for _, part := range strings.Split(server, ";") {
			name, value, ok := strings.Cut(part, "=")
			if !ok {
				continue
			}
			name = strings.TrimSpace(strings.ToLower(name))
			if name == "socks" || name == "socks5" {
				return parseWindowsProxyValue(name, value)
			}
		}
		return nil, false, nil
	}

	return parseWindowsProxyValue("http", server)
}

func parseWindowsProxyValue(kind string, rawServer string) (*url.URL, bool, error) {
	if strings.TrimSpace(rawServer) == "" {
		return nil, false, nil
	}

	fallbackScheme := "http"
	if strings.EqualFold(kind, "socks") || strings.EqualFold(kind, "socks5") {
		fallbackScheme = "socks5"
	}

	parsed, err := parseProxyAddress(rawServer, fallbackScheme)
	if err != nil {
		return nil, true, err
	}
	return parsed, true, nil
}
