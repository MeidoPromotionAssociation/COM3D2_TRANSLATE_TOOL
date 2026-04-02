package translation

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"COM3D2TranslateTool/internal/model"
)

func TestProxyConnectivity(ctx context.Context, proxy model.ProxyConfig, targetURL string) (model.ProxyTestResult, error) {
	targetURL = strings.TrimSpace(targetURL)
	if targetURL == "" {
		return model.ProxyTestResult{}, fmt.Errorf("target url is required")
	}

	client, err := newHTTPClient(proxy, 15, 15*time.Second)
	if err != nil {
		return model.ProxyTestResult{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return model.ProxyTestResult{}, err
	}

	resolvedProxy := ""
	proxyFunc, err := resolveProxyFunc(proxy)
	if err != nil {
		return model.ProxyTestResult{}, err
	}
	if proxyFunc != nil {
		proxyURL, err := proxyFunc(req)
		if err != nil {
			return model.ProxyTestResult{}, err
		}
		if proxyURL != nil {
			resolvedProxy = proxyURL.String()
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return model.ProxyTestResult{}, err
	}
	defer resp.Body.Close()

	bytesRead, readErr := io.Copy(io.Discard, io.LimitReader(resp.Body, 8*1024))
	if readErr != nil {
		return model.ProxyTestResult{}, readErr
	}

	finalURL := targetURL
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}

	return model.ProxyTestResult{
		TargetURL:     targetURL,
		FinalURL:      finalURL,
		ProxyMode:     normalizeProxyMode(proxy.Mode),
		ResolvedProxy: resolvedProxy,
		StatusCode:    resp.StatusCode,
		Status:        resp.Status,
		BytesRead:     bytesRead,
	}, nil
}
