//go:build !windows

package translation

import (
	"net/http"
	"net/url"
)

func systemProxyFunc() func(*http.Request) (*url.URL, error) {
	return http.ProxyFromEnvironment
}
